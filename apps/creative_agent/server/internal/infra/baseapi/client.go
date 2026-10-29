package baseapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"
)

// Client is the HTTP client of base-server's /inner/creation/*.
// Every request injects the traceparent header (W3C trace propagation).
type Client struct {
	baseURL string
	http    *http.Client
}

// ClientConfig holds the Client's dependencies.
type ClientConfig struct {
	BaseURL string       // base-server address, e.g. http://localhost:8081
	HTTP    *http.Client // optional; when nil, a default client with a 30s timeout is used
}

// NewClient creates the base-server client.
func NewClient(cfg ClientConfig) *Client {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: cfg.BaseURL, http: cfg.HTTP}
}

// BaseURL returns the base-server address (for callers to check whether it is configured).
func (c *Client) BaseURL() string { return c.baseURL }

// PreCreate calls /inner/creation/pre_create.
func (c *Client) PreCreate(ctx context.Context, req *PreCreateReq) (*PreCreateResp, error) {
	var resp PreCreateResp
	if err := c.do(ctx, "POST", "/inner/creation/pre_create", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Create calls /inner/creation/create.
func (c *Client) Create(ctx context.Context, req *CreateReq) (*CreateResp, error) {
	var resp CreateResp
	if err := c.do(ctx, "POST", "/inner/creation/create", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AsyncPostCreate calls /inner/creation/async_post_create.
func (c *Client) AsyncPostCreate(ctx context.Context, req *AsyncPostCreateReq) (*AsyncPostCreateResp, error) {
	var resp AsyncPostCreateResp
	if err := c.do(ctx, "POST", "/inner/creation/async_post_create", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// do performs one HTTP request: inject the traceparent header → POST JSON → parse the response.
func (c *Client) do(ctx context.Context, method, path string, req, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("baseapi marshal %s: %w", path, err)
	}

	hreq, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("baseapi build request %s: %w", path, err)
	}
	hreq.Header.Set("Content-Type", "application/json")

	// W3C trace propagation: extract the span from ctx and serialize it into the traceparent header.
	carrier := tracex.InjectCarrier(ctx)
	if tp, ok := carrier[tracex.TraceParentKey]; ok {
		hreq.Header.Set(tracex.TraceParentKey, tp)
	}

	hresp, err := c.http.Do(hreq)
	if err != nil {
		return fmt.Errorf("baseapi do %s: %w", path, err)
	}
	defer hresp.Body.Close()

	out, err := io.ReadAll(hresp.Body)
	if err != nil {
		return fmt.Errorf("baseapi read %s: %w", path, err)
	}
	if hresp.StatusCode >= 400 {
		return fmt.Errorf("baseapi http %d for %s: %s", hresp.StatusCode, path, string(out))
	}
	if err := json.Unmarshal(out, resp); err != nil {
		return fmt.Errorf("baseapi unmarshal %s: %w", path, err)
	}
	// base-server contract: HTTP 2xx with errno != 0 means the API call failed and Data is unusable (may be nil).
	// Business failure cards (e.g. insufficient points) are errno=0 + status!=0; they go through the status branch and return normally, not as errors here.
	if errno := responseErrno(out); errno != 0 {
		return fmt.Errorf("baseapi business errno %d for %s: %s", errno, path, string(out))
	}
	return nil
}

// responseErrno parses the top-level errno from the response body; returns 0 when unparsable (treated as success, leaving parsing to the caller).
func responseErrno(out []byte) int {
	var env struct {
		Errno int `json:"errno"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return 0
	}
	return env.Errno
}
