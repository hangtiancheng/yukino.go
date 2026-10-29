package ideaking

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BaiduHotProvider pulls realtime hot searches from the Baidu hot board (top.baidu.com public API) as the hotspot source.
//
// Data source: GET https://top.baidu.com/api/board?platform=wise&tab=realtime
// Each element of data.cards[0].content[].content[] has word (title) / index / hotTag / url.
// Baidu hot searches have titles but no description text, so text uses the title itself; the eid is md5(word) (stable for echo/locating).
// When query is non-empty, hot-search titles are matched by keyword (fuzzy); without matches it falls back to the top N.
//
// No base_url configuration is needed (built-in public API); on network failure the caller (app wiring layer) falls back to MockProvider.
type BaiduHotProvider struct {
	http *http.Client
}

// NewBaiduHotProvider creates a BaiduHotProvider.
func NewBaiduHotProvider() *BaiduHotProvider {
	return &BaiduHotProvider{http: &http.Client{Timeout: 10 * time.Second}}
}

const defaultBaiduHotURL = "https://top.baidu.com/api/board?platform=wise&tab=realtime"

// baiduHotURL is the Baidu hot-search API address (a package variable, overwritable in tests).
var baiduHotURL = defaultBaiduHotURL

// BaiduHotURL returns the current Baidu hot-search API address (readable by tests).
func BaiduHotURL() string { return baiduHotURL }

// SetBaiduHotURL overwrites the Baidu hot-search API address (for tests; pass the original value to restore).
func SetBaiduHotURL(u string) { baiduHotURL = u }

// baiduHotResp is the minimal structure of the Baidu hot-search API response (only the needed fields).
type baiduHotResp struct {
	Success bool `json:"success"`
	Data    struct {
		Cards []struct {
			Content []struct {
				Content []struct {
					Word   string `json:"word"`
					Index  int    `json:"index"`
					HotTag string `json:"hotTag"`
				} `json:"content"`
			} `json:"content"`
		} `json:"cards"`
	} `json:"data"`
}

// SearchHotTopics pulls Baidu realtime hot searches and returns the topic list with eids + the hotspot summary markdown.
func (p *BaiduHotProvider) SearchHotTopics(ctx context.Context, query string, count int) (*HotTopicResult, error) {
	if count <= 0 {
		count = 5
	}
	if count > 10 {
		count = 10
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baiduHotURL, nil)
	if err != nil {
		return nil, fmt.Errorf("baidu_hot: new request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("baidu_hot: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("baidu_hot: status %d: %s", resp.StatusCode, string(raw))
	}

	var out baiduHotResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("baidu_hot: decode response: %w", err)
	}
	if !out.Success {
		return nil, fmt.Errorf("baidu_hot: api success=false")
	}

	// Extract the hot-search title list
	var words []string
	for _, card := range out.Data.Cards {
		for _, c := range card.Content {
			for _, item := range c.Content {
				if item.Word != "" {
					words = append(words, item.Word)
				}
			}
		}
	}
	if len(words) == 0 {
		return nil, fmt.Errorf("baidu_hot: empty hot list")
	}

	// When query is non-empty, match hot-search titles by keyword; without matches fall back to the first count entries
	selected := words
	if query != "" {
		kw := strings.TrimSpace(query)
		var matched []string
		for _, w := range words {
			if strings.Contains(w, kw) || strings.Contains(kw, w) {
				matched = append(matched, w)
			}
		}
		if len(matched) > 0 {
			selected = matched
		}
	}
	if len(selected) > count {
		selected = selected[:count]
	}

	outRes := &HotTopicResult{
		HotspotData: fmt.Sprintf("为你整理了 %d 个实时热搜选题，挑一个开工吧～", len(selected)),
	}
	for _, w := range selected {
		h := md5.Sum([]byte(w))
		outRes.TitleData = append(outRes.TitleData, TitleItem{
			Title:   w,
			Content: "实时热搜：可围绕该话题创作爆款内容。",
			Eid:     hex.EncodeToString(h[:8]),
		})
	}
	return outRes, nil
}

// FallbackProvider combines a primary provider with a fallback provider: when the primary fails (e.g. a Baidu hot-search
// network error) it automatically degrades to the mock, guaranteeing the demo always runs.
type FallbackProvider struct {
	Primary  IdeaKingProvider
	Fallback IdeaKingProvider
}

// NewFallbackProvider creates the combined provider.
func NewFallbackProvider(primary, fallback IdeaKingProvider) *FallbackProvider {
	return &FallbackProvider{Primary: primary, Fallback: fallback}
}

// SearchHotTopics tries the primary provider first and degrades to the fallback on failure.
func (p *FallbackProvider) SearchHotTopics(ctx context.Context, query string, count int) (*HotTopicResult, error) {
	res, err := p.Primary.SearchHotTopics(ctx, query, count)
	if err == nil && res != nil && len(res.TitleData) > 0 {
		return res, nil
	}
	if p.Fallback != nil {
		return p.Fallback.SearchHotTopics(ctx, query, count)
	}
	return res, err
}
