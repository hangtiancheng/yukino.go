// Package upstream uses the official SDKs without dropping unknown wire fields.
package upstream

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go"
	openaioption "github.com/openai/openai-go/option"

	"github.com/hangtiancheng/yukino.go/apps/agent/server_proxy/internal/config"
)

type Client struct {
	Provider  config.Provider
	openai    openai.Client
	anthropic anthropic.Client
	path      string
}

func New(p config.Provider) *Client {
	c := &Client{Provider: p}
	httpClient := &http.Client{Timeout: 10 * time.Minute}
	base := strings.TrimRight(p.BaseURL, "/")
	if p.Protocol == config.Anthropic {
		c.path = "v1/messages"
		if strings.HasSuffix(base, "/v1") {
			c.path = "messages"
		}
		if strings.HasSuffix(base, "/messages") {
			base = strings.TrimSuffix(base, "/messages")
			c.path = "messages"
		}
		c.anthropic = anthropic.NewClient(anthropicoption.WithBaseURL(base+"/"), anthropicoption.WithAPIKey(p.APIKey), anthropicoption.WithAuthToken(""), anthropicoption.WithHTTPClient(httpClient), anthropicoption.WithMaxRetries(0))
	} else {
		c.path = "chat/completions"
		if p.Protocol == config.OpenAI {
			c.path = "responses"
		}
		base = strings.TrimSuffix(base, "/"+c.path)
		u, _ := url.Parse(base)
		if u.Path == "" || u.Path == "/" {
			base += "/v1"
		}
		c.openai = openai.NewClient(openaioption.WithBaseURL(base+"/"), openaioption.WithAPIKey(p.APIKey), openaioption.WithHTTPClient(httpClient), openaioption.WithMaxRetries(0))
	}
	return c
}

// Messages posts an Anthropic Messages request used by the Claude agent.
func (c *Client) Messages(ctx context.Context, body map[string]any, headers http.Header) (*http.Response, error) {
	var response *http.Response
	if c.Provider.Protocol == config.Anthropic {
		err := c.anthropic.Post(ctx, c.path, body, &response, nativeHeaders(headers)...)
		return response, err
	}
	err := c.openai.Post(ctx, c.path, body, &response, openaioption.WithHeader("Accept-Encoding", "identity"))
	return response, err
}

// CountTokens posts a native Anthropic token-count request.
func (c *Client) CountTokens(ctx context.Context, body map[string]any, headers http.Header) (*http.Response, error) {
	var response *http.Response
	err := c.anthropic.Post(ctx, c.path+"/count_tokens", body, &response, nativeHeaders(headers)...)
	return response, err
}

// Responses posts an OpenAI Responses request used by the Codex agent. A
// compact request targets the Responses compaction route on native OpenAI.
func (c *Client) Responses(ctx context.Context, body map[string]any, headers http.Header, compact bool) (*http.Response, error) {
	var response *http.Response
	if c.Provider.Protocol == config.Anthropic {
		err := c.anthropic.Post(ctx, c.path, body, &response, nativeHeaders(headers)...)
		return response, err
	}
	path := c.path
	if compact && c.Provider.Protocol == config.OpenAI {
		path += "/compact"
	}
	err := c.openai.Post(ctx, path, body, &response, openaioption.WithHeader("Accept-Encoding", "identity"))
	return response, err
}

func nativeHeaders(headers http.Header) []anthropicoption.RequestOption {
	opts := []anthropicoption.RequestOption{anthropicoption.WithHeader("Accept-Encoding", "identity"), anthropicoption.WithHeaderDel("Authorization")}
	for _, key := range []string{"anthropic-version", "anthropic-beta"} {
		if value := headers.Get(key); value != "" {
			opts = append(opts, anthropicoption.WithHeader(key, value))
		}
	}
	return opts
}
