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

// Client 是 base-server /inner/creation/* 的 HTTP 客户端。
// 每次请求注入 traceparent 头（W3C trace 透传）。
type Client struct {
	baseURL string
	http    *http.Client
}

// ClientConfig 持有 Client 的依赖。
type ClientConfig struct {
	BaseURL string       // base-server 地址，如 http://localhost:8081
	HTTP    *http.Client // 可选；nil 时用 30s 超时默认 client
}

// NewClient 创建 base-server 客户端。
func NewClient(cfg ClientConfig) *Client {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: cfg.BaseURL, http: cfg.HTTP}
}

// BaseURL 返回 base-server 地址（供调用方判断是否配置）。
func (c *Client) BaseURL() string { return c.baseURL }

// PreCreate 调 /inner/creation/pre_create。
func (c *Client) PreCreate(ctx context.Context, req *PreCreateReq) (*PreCreateResp, error) {
	var resp PreCreateResp
	if err := c.do(ctx, "POST", "/inner/creation/pre_create", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Create 调 /inner/creation/create。
func (c *Client) Create(ctx context.Context, req *CreateReq) (*CreateResp, error) {
	var resp CreateResp
	if err := c.do(ctx, "POST", "/inner/creation/create", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// AsyncPostCreate 调 /inner/creation/async_post_create。
func (c *Client) AsyncPostCreate(ctx context.Context, req *AsyncPostCreateReq) (*AsyncPostCreateResp, error) {
	var resp AsyncPostCreateResp
	if err := c.do(ctx, "POST", "/inner/creation/async_post_create", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// do 执行一次 HTTP 请求：注入 traceparent 头 → POST JSON → 解析响应。
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

	// W3C trace 透传：从 ctx 提取 span 序列化为 traceparent 头。
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
	// base-server 契约：HTTP 2xx 但 errno != 0 表示接口调用失败，Data 不可用（可能为 nil）。
	// 业务失败卡（如积分不足）是 errno=0 + status!=0，走 status 分支正常返回，不在此报错。
	if errno := responseErrno(out); errno != 0 {
		return fmt.Errorf("baseapi business errno %d for %s: %s", errno, path, string(out))
	}
	return nil
}

// responseErrno 从响应体解出顶层 errno；解不出时返回 0（视为成功，交给调用方解析）。
func responseErrno(out []byte) int {
	var env struct {
		Errno int `json:"errno"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return 0
	}
	return env.Errno
}
