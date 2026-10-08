package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/claude/bridge"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/upstream"
)

const CheckTimeout = 10 * time.Second

// Check verifies the selected inference route, credentials, and model before
// settings or an existing service are changed. Abort after the first valid
// stream event; the request has a small output budget and no tools.
func Check(ctx context.Context, c *upstream.Client) error {
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	body := bridge.Object{"max_tokens": int64(16), "stream": true, "messages": []any{bridge.Object{"role": "user", "content": "Reply OK."}}}
	if c.Provider.Protocol != config.Anthropic {
		body["thinking"] = bridge.Object{"type": "disabled"}
	}
	request, err := bridge.Request(body, c.Provider)
	if err != nil {
		return fmt.Errorf("provider %q connection check could not create a request", c.Provider.Name)
	}
	response, err := c.Messages(ctx, request, nil)
	if response != nil {
		defer response.Body.Close()
	}
	if response != nil && (response.StatusCode < 200 || response.StatusCode >= 300) {
		return fmt.Errorf("provider %q connection check failed: upstream returned HTTP %d", c.Provider.Name, response.StatusCode)
	}
	if err != nil || response == nil {
		if ctx.Err() != nil {
			return fmt.Errorf("provider %q connection check was cancelled or timed out", c.Provider.Name)
		}
		return fmt.Errorf("provider %q connection check failed: unable to reach the inference endpoint", c.Provider.Name)
	}
	reader := bufio.NewReader(response.Body)
	for {
		first, err := reader.Peek(1)
		if err != nil {
			return fmt.Errorf("provider %q connection check failed: empty response", c.Provider.Name)
		}
		if !strings.ContainsRune(" \t\r\n", rune(first[0])) {
			break
		}
		_, _ = reader.ReadByte()
	}
	first, _ := reader.Peek(1)
	if first[0] == '{' {
		var result bridge.Object
		err := json.NewDecoder(io.LimitReader(reader, 1<<20)).Decode(&result)
		if err == nil {
			_, err = bridge.Response(result, c.Provider.Protocol, c.Provider.Model)
		}
		if err == nil && c.Provider.Protocol == config.Anthropic && result["type"] != "message" {
			err = fmt.Errorf("invalid native response")
		}
		if err != nil {
			return fmt.Errorf("provider %q connection check failed: invalid inference response", c.Provider.Name)
		}
		return nil
	}
	valid := false
	err = bridge.ReadSSE(reader, func(event, data string) (bool, error) {
		var result bridge.Object
		if json.Unmarshal([]byte(data), &result) != nil {
			return false, fmt.Errorf("invalid event")
		}
		kind := bridge.Str(result["type"])
		if kind == "" {
			kind = event
		}
		if bridge.Obj(result["error"]) != nil || kind == "error" || kind == "response.failed" || kind == "response.cancelled" {
			return false, fmt.Errorf("upstream stream error")
		}
		switch c.Provider.Protocol {
		case config.Anthropic:
			valid = kind == "message_start" && bridge.Obj(result["message"])["type"] == "message"
		case config.OpenAICompat:
			choices := bridge.List(result["choices"])
			valid = len(choices) > 0 && bridge.Obj(bridge.Obj(choices[0])["delta"]) != nil
		case config.OpenAI:
			envelope := bridge.Obj(result["response"])
			if envelope["status"] == "failed" || envelope["status"] == "cancelled" || bridge.Obj(envelope["error"]) != nil {
				return false, fmt.Errorf("upstream response error")
			}
			valid = (kind == "response.created" || kind == "response.in_progress" || kind == "response.completed") && envelope != nil
		}
		return !valid, nil
	})
	if err != nil || !valid {
		return fmt.Errorf("provider %q connection check failed: invalid or failed inference stream", c.Provider.Name)
	}
	return nil
}
