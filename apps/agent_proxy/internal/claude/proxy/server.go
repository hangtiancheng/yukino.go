package proxy

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	httpapp "github.com/hangtiancheng/yukino.go/libs/yukino_http"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/claude/bridge"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/upstream"
)

const MaxBody = 32 * 1024 * 1024

func New(p config.Provider) *httpapp.Application {
	app := httpapp.New()
	app.Use(httpapp.Recovery())
	client := upstream.New(p)
	messages := func(c *httpapp.Context, _ func()) { handleMessages(c, client) }
	app.Post("/v1/messages", messages)
	app.Post("/messages", messages)
	app.Post("/v1/messages/count_tokens", func(c *httpapp.Context, _ func()) {
		if p.Protocol != config.Anthropic {
			fail(c, 501, "api_error", "Token counting is unavailable for this upstream protocol.")
			return
		}
		body, err := readRequest(c)
		if err != nil {
			fail(c, 400, "invalid_request_error", err.Error())
			return
		}
		body["model"] = p.Model
		response, err := client.CountTokens(c.Request.Context(), body, c.Request.Header)
		if response == nil {
			fail(c, 502, "api_error", "Upstream token-count request failed.")
			return
		}
		defer response.Body.Close()
		if err != nil || response.StatusCode >= 400 {
			upstreamError(c, response, p.APIKey)
			return
		}
		data, err := readBody(response.Body)
		if err != nil {
			fail(c, 502, "api_error", err.Error())
			return
		}
		c.SetStatus(response.StatusCode)
		c.Set("Content-Type", "application/json")
		c.Data(data)
	})
	app.Get("/health", func(c *httpapp.Context, _ func()) {
		c.JSON(bridge.Object{"status": "ok", "service": "yukino-agent-proxy"})
	})
	app.Get("/v1/models", func(c *httpapp.Context, _ func()) {
		c.JSON(bridge.Object{"data": []any{bridge.Object{"id": p.Model, "type": "model", "display_name": p.Model}}, "has_more": false, "first_id": p.Model, "last_id": p.Model})
	})
	return app
}

func readRequest(c *httpapp.Context) (bridge.Object, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBody)
	decoder := json.NewDecoder(c.Request.Body)
	var body bridge.Object
	if err := decoder.Decode(&body); err != nil || body == nil {
		return nil, fmt.Errorf("Request body must be a JSON object no larger than 32 MiB.")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("Request body must contain exactly one JSON object.")
	}
	return body, nil
}

func handleMessages(c *httpapp.Context, client *upstream.Client) {
	body, err := readRequest(c)
	if err != nil {
		fail(c, 400, "invalid_request_error", err.Error())
		return
	}
	stream, _ := body["stream"].(bool)
	if value, exists := body["stream"]; exists {
		if _, ok := value.(bool); !ok {
			fail(c, 400, "invalid_request_error", "stream must be a boolean.")
			return
		}
	}
	request, err := bridge.Request(body, client.Provider)
	if err != nil {
		fail(c, 400, "invalid_request_error", err.Error())
		return
	}
	response, err := client.Messages(c.Request.Context(), request, c.Request.Header)
	if response == nil {
		if c.Request.Context().Err() == nil {
			fail(c, 502, "api_error", "Upstream request failed.")
		}
		return
	}
	defer response.Body.Close()
	if err != nil || response.StatusCode >= 400 {
		upstreamError(c, response, client.Provider.APIKey)
		return
	}
	reader := bufio.NewReader(response.Body)
	isSSE := strings.Contains(response.Header.Get("Content-Type"), "text/event-stream")
	if !stream && !isSSE {
		for {
			b, e := reader.Peek(1)
			if e != nil {
				break
			}
			if strings.ContainsRune(" \t\r\n", rune(b[0])) {
				_, _ = reader.ReadByte()
				continue
			}
			isSSE = b[0] != '{' && b[0] != '['
			break
		}
	}
	if stream {
		sse := c.SSE()
		stopHeartbeat := sse.Heartbeat(15 * time.Second)
		defer stopHeartbeat()
		sink := func(event string, data bridge.Object) error {
			if err := c.Request.Context().Err(); err != nil {
				return err
			}
			sse.JSON(event, data)
			return nil
		}
		if err := bridge.Stream(reader, client.Provider.Protocol, client.Provider.Model, sink); err != nil && c.Request.Context().Err() == nil {
			sse.JSON("error", bridge.Object{"type": "error", "error": bridge.Object{"type": "api_error", "message": redact(err.Error(), client.Provider.APIKey)}})
		}
		return
	}
	if isSSE {
		var collector bridge.Collector
		if err := bridge.Stream(reader, client.Provider.Protocol, client.Provider.Model, collector.Sink); err != nil {
			fail(c, 502, "api_error", redact(err.Error(), client.Provider.APIKey))
			return
		}
		message, err := collector.Result()
		if err != nil {
			fail(c, 502, "api_error", err.Error())
			return
		}
		c.JSON(message)
		return
	}
	data, err := readBody(reader)
	if err != nil {
		fail(c, 502, "api_error", err.Error())
		return
	}
	var upstreamBody bridge.Object
	if json.Unmarshal(data, &upstreamBody) != nil || upstreamBody == nil {
		fail(c, 502, "api_error", "Upstream response is not a JSON object.")
		return
	}
	message, err := bridge.Response(upstreamBody, client.Provider.Protocol, client.Provider.Model)
	if err != nil {
		fail(c, 502, "api_error", redact(err.Error(), client.Provider.APIKey))
		return
	}
	if client.Provider.Protocol == config.Anthropic {
		c.Set("Content-Type", "application/json")
		c.Data(data)
	} else {
		c.JSON(message)
	}
}

func readBody(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("Unable to read upstream response.")
	}
	if len(data) > MaxBody {
		return nil, fmt.Errorf("Upstream response exceeds 32 MiB.")
	}
	return data, nil
}
func redact(message, key string) string {
	if key != "" {
		return strings.ReplaceAll(message, key, "[redacted]")
	}
	return message
}
func fail(c *httpapp.Context, status int, kind, message string) {
	c.SetStatus(status)
	c.JSON(bridge.Object{"type": "error", "error": bridge.Object{"type": kind, "message": message}})
}
func upstreamError(c *httpapp.Context, response *http.Response, key string) {
	status := response.StatusCode
	if status < 400 {
		status = 502
	}
	kind := "api_error"
	switch status {
	case 400, 404, 422:
		kind = "invalid_request_error"
	case 401:
		kind = "authentication_error"
	case 403:
		kind = "permission_error"
	case 429:
		kind = "rate_limit_error"
	case 529:
		kind = "overloaded_error"
	}
	message := fmt.Sprintf("Upstream returned HTTP %d.", response.StatusCode)
	if data, err := readBody(response.Body); err == nil {
		var value bridge.Object
		if json.Unmarshal(data, &value) == nil {
			if text := bridge.Str(bridge.Obj(value["error"])["message"]); text != "" {
				message = text
			}
		}
	}
	if retry := response.Header.Get("Retry-After"); retry != "" {
		c.Set("Retry-After", retry)
	}
	fail(c, status, kind, redact(message, key))
}
