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

// BaiduHotProvider 从百度热搜榜（top.baidu.com 公开接口）拉取实时热搜作为热点源。
//
// 数据源：GET https://top.baidu.com/api/board?platform=wise&tab=realtime
// 响应 data.cards[0].content[].content[] 每个元素有 word（标题）/ index / hotTag / url。
// 百度热搜只有标题没有描述文本，故 text 用标题本身；eid 用 word 的 md5（稳定回传定位）。
// query 非空时按关键词匹配热搜标题（模糊匹配），无匹配回退 top N。
//
// 无需配置 base_url（内置公开接口）；网络失败由调用方（app 装配层）fallback 到 MockProvider。
type BaiduHotProvider struct {
	http *http.Client
}

// NewBaiduHotProvider 创建 BaiduHotProvider。
func NewBaiduHotProvider() *BaiduHotProvider {
	return &BaiduHotProvider{http: &http.Client{Timeout: 10 * time.Second}}
}

const defaultBaiduHotURL = "https://top.baidu.com/api/board?platform=wise&tab=realtime"

// baiduHotURL 是百度热搜接口地址（包内变量，测试可覆写）。
var baiduHotURL = defaultBaiduHotURL

// BaiduHotURL 返回当前百度热搜接口地址（测试可读）。
func BaiduHotURL() string { return baiduHotURL }

// SetBaiduHotURL 覆写百度热搜接口地址（测试用；恢复时传原值）。
func SetBaiduHotURL(u string) { baiduHotURL = u }

// baiduHotResp 是百度热搜接口响应的最小结构（只取需要的字段）。
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

// SearchHotTopics 拉百度实时热搜，返回带 eid 的选题列表 + 热点摘要 markdown。
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

	// 提取热搜标题列表
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

	// query 非空时按关键词匹配热搜标题；无匹配回退前 count 条
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

// FallbackProvider 组合主 provider 与降级 provider：主 provider 失败（如百度热搜
// 网络异常）时自动降级到 mock，保证 demo 恒可跑。
type FallbackProvider struct {
	Primary  IdeaKingProvider
	Fallback IdeaKingProvider
}

// NewFallbackProvider 创建组合 provider。
func NewFallbackProvider(primary, fallback IdeaKingProvider) *FallbackProvider {
	return &FallbackProvider{Primary: primary, Fallback: fallback}
}

// SearchHotTopics 先试主 provider，失败降级到 fallback。
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
