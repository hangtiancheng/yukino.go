package proxy

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/codex/bridge"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/upstream"
	httpapp "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"github.com/klauspost/compress/zstd"
)

const MaxBody = 32 * 1024 * 1024

func New(p config.Provider) *httpapp.Application {
	app := httpapp.New()
	app.Use(httpapp.Recovery())
	client := upstream.New(p)
	history := &bridge.History{}
	for _, path := range []string{"/responses", "/v1/responses"} {
		app.Post(path, func(c *httpapp.Context, _ func()) { handleResponses(c, client, history, false) })
		app.Get(path, func(c *httpapp.Context, _ func()) {
			fail(c, 426, "invalid_request_error", "websocket_not_supported", "Use Responses over HTTP/SSE; WebSocket transport is not supported.")
		})
	}
	for _, path := range []string{"/responses/compact", "/v1/responses/compact"} {
		app.Post(path, func(c *httpapp.Context, _ func()) { handleResponses(c, client, history, true) })
	}
	app.Get("/health", func(c *httpapp.Context, _ func()) {
		c.JSON(bridge.Object{"status": "ok", "service": "yukino-agent-proxy"})
	})
	for _, path := range []string{"/models", "/v1/models"} {
		app.Get(path, func(c *httpapp.Context, _ func()) {
			c.JSON(bridge.Object{"object": "list", "data": []any{bridge.Object{"id": p.Model, "object": "model", "created": 0, "owned_by": p.Name}}})
		})
	}
	return app
}

func readRequest(c *httpapp.Context) (bridge.Object, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxBody)
	var reader io.Reader = c.Request.Body
	switch strings.ToLower(c.Request.Header.Get("Content-Encoding")) {
	case "", "identity":
	case "gzip":
		r, err := gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("invalid gzip request body")
		}
		defer r.Close()
		reader = r
	case "zstd":
		r, err := zstd.NewReader(reader, zstd.WithDecoderMaxMemory(64<<20), zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, fmt.Errorf("invalid zstd request body")
		}
		defer r.Close()
		reader = r
	default:
		return nil, fmt.Errorf("unsupported request Content-Encoding")
	}
	data, err := readBody(reader)
	if err != nil {
		return nil, fmt.Errorf("request body must be no larger than 32 MiB")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var body bridge.Object
	if decoder.Decode(&body) != nil || body == nil {
		return nil, fmt.Errorf("request body must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("request body must contain exactly one JSON object")
	}
	if value, exists := body["stream"]; exists {
		if _, ok := value.(bool); !ok {
			return nil, fmt.Errorf("stream must be a boolean")
		}
	}
	return body, nil
}

func handleResponses(c *httpapp.Context, client *upstream.Client, history *bridge.History, compact bool) {
	body, err := readRequest(c)
	if err != nil {
		fail(c, 400, "invalid_request_error", "invalid_request", err.Error())
		return
	}
	if client.Provider.Protocol != config.OpenAI {
		if err := history.Enrich(body); err != nil {
			fail(c, 400, "invalid_request_error", "history_unavailable", err.Error())
			return
		}
	}
	if compact && client.Provider.Protocol != config.OpenAI {
		body["input"] = append(bridge.InputItems(body["input"]), bridge.Object{"type": "compaction_trigger"})
		body["stream"] = false
	}
	stream := body["stream"] == true
	request, tools, err := bridge.Request(body, client.Provider)
	if err != nil {
		fail(c, 400, "invalid_request_error", "invalid_request", err.Error())
		return
	}
	response, err := client.Responses(c.Request.Context(), request, c.Request.Header, compact)
	if response == nil {
		if c.Request.Context().Err() == nil {
			fail(c, 502, "api_error", "upstream_error", "Upstream request failed.")
		}
		return
	}
	defer response.Body.Close()
	if err != nil || response.StatusCode >= 400 {
		upstreamError(c, response, client.Provider.APIKey)
		return
	}
	reader := bufio.NewReader(response.Body)
	for {
		first, err := reader.Peek(1)
		if err != nil {
			fail(c, 502, "api_error", "upstream_error", "Upstream returned an empty response.")
			return
		}
		if !strings.ContainsRune(" \t\r\n", rune(first[0])) {
			break
		}
		_, _ = reader.ReadByte()
	}
	first, _ := reader.Peek(1)
	isJSON := first[0] == '{'
	if stream {
		var converted bridge.Object
		if isJSON {
			data, err := readBody(reader)
			if err == nil {
				var value bridge.Object
				value, err = bridge.Decode(data)
				if err == nil {
					converted, err = bridge.Response(value, client.Provider.Protocol, client.Provider.Model, tools)
				}
			}
			if err != nil {
				fail(c, 502, "api_error", "upstream_error", redact(err.Error(), client.Provider.APIKey))
				return
			}
		}
		sse := c.SSE()
		heartbeat := sse.Heartbeat(15 * time.Second)
		defer heartbeat()
		var collector bridge.Collector
		sink := func(event string, data bridge.Object) error {
			if err := c.Request.Context().Err(); err != nil {
				return err
			}
			if err := collector.Sink(event, data); err != nil {
				return err
			}
			sse.JSON(event, data)
			return nil
		}
		if isJSON {
			err = bridge.ResponseSSE(converted, sink)
		} else {
			err = bridge.Stream(reader, client.Provider.Protocol, client.Provider.Model, tools, sink)
		}
		if err != nil && c.Request.Context().Err() == nil {
			sse.JSON("response.failed", bridge.Object{"type": "response.failed", "response": bridge.Object{"id": bridge.ID("resp_"), "object": "response", "status": "failed", "output": []any{}, "error": bridge.Object{"type": "api_error", "code": "upstream_error", "message": redact(err.Error(), client.Provider.APIKey)}}})
			return
		}
		if final, err := collector.Result(); err == nil && client.Provider.Protocol != config.OpenAI {
			history.Record(body, final)
		}
		return
	}
	var result bridge.Object
	if isJSON {
		data, err := readBody(reader)
		if err == nil {
			result, err = bridge.Decode(data)
		}
		if err == nil && compact && client.Provider.Protocol == config.OpenAI {
			if result["error"] != nil || result["object"] != "response.compaction" || bridge.List(result["output"]) == nil {
				err = fmt.Errorf("upstream returned an invalid compaction response")
			}
		} else if err == nil {
			result, err = bridge.Response(result, client.Provider.Protocol, client.Provider.Model, tools)
		}
		if err != nil {
			fail(c, 502, "api_error", "upstream_error", redact(err.Error(), client.Provider.APIKey))
			return
		}
	} else {
		var collector bridge.Collector
		if err := bridge.Stream(reader, client.Provider.Protocol, client.Provider.Model, tools, collector.Sink); err != nil {
			fail(c, 502, "api_error", "upstream_error", redact(err.Error(), client.Provider.APIKey))
			return
		}
		result, err = collector.Result()
		if err != nil {
			fail(c, 502, "api_error", "upstream_error", err.Error())
			return
		}
	}
	if compact {
		result["object"] = "response.compaction"
	}
	if client.Provider.Protocol != config.OpenAI {
		history.Record(body, result)
	}
	c.JSON(result)
}

func readBody(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("unable to read upstream response")
	}
	if len(data) > MaxBody {
		return nil, fmt.Errorf("upstream response exceeds 32 MiB")
	}
	return data, nil
}
func redact(message, key string) string {
	if key != "" {
		return strings.ReplaceAll(message, key, "[redacted]")
	}
	return message
}
func fail(c *httpapp.Context, status int, kind, code, message string) {
	c.SetStatus(status)
	c.JSON(bridge.Object{"error": bridge.Object{"type": kind, "code": code, "message": message, "param": nil}})
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
	}
	message := fmt.Sprintf("Upstream returned HTTP %d.", response.StatusCode)
	if data, err := readBody(response.Body); err == nil {
		if value, err := bridge.Decode(data); err == nil {
			if text := bridge.Str(bridge.Obj(value["error"])["message"]); text != "" {
				message = text
			}
		}
	}
	if retry := response.Header.Get("Retry-After"); retry != "" {
		c.Set("Retry-After", retry)
	}
	fail(c, status, kind, "upstream_error", redact(message, key))
}
