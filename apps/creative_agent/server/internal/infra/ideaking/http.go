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

// HTTPProvider is the HTTP implementation connecting to a real hotspot service.
// It requests {baseURL}/inner/ideaking/hot_topics with body {"query","hotspot_count"}.
// It reuses the baseapi pattern: traceparent propagation + 30s timeout + 4xx/business-errno error detection.
type HTTPProvider struct {
	baseURL string
	http    *http.Client
}

// HTTPConfig holds the HTTPProvider's dependencies.
type HTTPConfig struct {
	BaseURL string       // hotspot service address, e.g. http://localhost:8085
	HTTP    *http.Client // optional; when nil, a default client with a 30s timeout is used
}

// NewHTTPProvider creates an HTTPProvider.
func NewHTTPProvider(cfg HTTPConfig) *HTTPProvider {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPProvider{baseURL: cfg.BaseURL, http: cfg.HTTP}
}

// hotTopicsReq is the hot-topic retrieval request body.
type hotTopicsReq struct {
	Query        string `json:"query"`
	HotspotCount int    `json:"hotspot_count"`
}

// hotTopicsResp is the hotspot service response (wrapped with errno/message).
type hotTopicsResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    *HotTopicResult `json:"data"`
}

// SearchHotTopics calls the external hotspot service to retrieve topics.
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
	// W3C traceparent cross-process propagation (aligned with baseapi).
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
