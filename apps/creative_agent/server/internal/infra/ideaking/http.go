package ideaking

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

// HTTPProvider 是接真实热点服务的 HTTP 实现。
// 请求 {baseURL}/inner/ideaking/hot_topics，body {"query","hotspot_count"}。
// 复用 baseapi 的模式：traceparent 透传 + 30s 超时 + 4xx/业务 errno 判错。
type HTTPProvider struct {
	baseURL string
	http    *http.Client
}

// HTTPConfig 持有 HTTPProvider 的依赖。
type HTTPConfig struct {
	BaseURL string       // 热点服务地址，如 http://localhost:8085
	HTTP    *http.Client // 可选；nil 时用 30s 超时默认 client
}

// NewHTTPProvider 创建 HTTPProvider。
func NewHTTPProvider(cfg HTTPConfig) *HTTPProvider {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPProvider{baseURL: cfg.BaseURL, http: cfg.HTTP}
}

// hotTopicsReq 是热点检索请求体。
type hotTopicsReq struct {
	Query        string `json:"query"`
	HotspotCount int    `json:"hotspot_count"`
}

// hotTopicsResp 是热点服务响应（外层包 errno/message）。
type hotTopicsResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    *HotTopicResult `json:"data"`
}

// SearchHotTopics 调外部热点服务检索选题。
func (p *HTTPProvider) SearchHotTopics(ctx context.Context, query string, count int) (*HotTopicResult, error) {
	body, err := json.Marshal(hotTopicsReq{Query: query, HotspotCount: count})
	if err != nil {
		return nil, fmt.Errorf("ideaking: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx,
		http.MethodPost, p.baseURL+"/inner/ideaking/hot_topics", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ideaking: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// W3C traceparent 跨进程透传（对齐 baseapi）。
	for k, v := range tracex.InjectCarrier(ctx) {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ideaking: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("ideaking: status %d: %s", resp.StatusCode, string(raw))
	}

	var out hotTopicsResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ideaking: decode response: %w", err)
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("ideaking: errno %d: %s", out.Code, out.Message)
	}
	if out.Data == nil {
		return nil, fmt.Errorf("ideaking: empty data")
	}
	return out.Data, nil
}
