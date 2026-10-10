package codex

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/codex/bridge"
	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/upstream"
)

const CheckTimeout = 20 * time.Second

func Check(ctx context.Context, c *upstream.Client) error {
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	body, tools, err := bridge.Request(bridge.Object{"input": "Reply with the single word OK.", "stream": true, "max_output_tokens": 128, "reasoning": bridge.Object{"effort": "none"}}, c.Provider)
	if err != nil {
		return fmt.Errorf("provider %q connection check could not create a request", c.Provider.Name)
	}
	response, err := c.Responses(ctx, body, nil, false)
	if response != nil {
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("provider %q connection check failed: upstream returned HTTP %d", c.Provider.Name, response.StatusCode)
		}
	}
	if err != nil || response == nil {
		return fmt.Errorf("provider %q connection check failed: unable to reach the inference endpoint", c.Provider.Name)
	}
	reader := bufio.NewReader(response.Body)
	for {
		first, err := reader.Peek(1)
		if err != nil {
			return fmt.Errorf("provider %q connection check failed: empty response", c.Provider.Name)
		}
		if first[0] != ' ' && first[0] != '\n' && first[0] != '\r' && first[0] != '\t' {
			break
		}
		_, _ = reader.ReadByte()
	}
	first, _ := reader.Peek(1)
	if first[0] == '{' {
		data, err := io.ReadAll(io.LimitReader(reader, 1<<20))
		if err == nil {
			var result bridge.Object
			result, err = bridge.Decode(data)
			if err == nil {
				_, err = bridge.Response(result, c.Provider.Protocol, c.Provider.Model, tools)
			}
		}
		if err != nil {
			return fmt.Errorf("provider %q connection check failed: invalid inference response", c.Provider.Name)
		}
		return nil
	}
	var collector bridge.Collector
	if err := bridge.Stream(reader, c.Provider.Protocol, c.Provider.Model, tools, collector.Sink); err != nil {
		return fmt.Errorf("provider %q connection check failed: invalid or failed inference stream", c.Provider.Name)
	}
	if _, err := collector.Result(); err != nil {
		return fmt.Errorf("provider %q connection check failed: incomplete inference stream", c.Provider.Name)
	}
	return nil
}
