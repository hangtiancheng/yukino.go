// Package ideaking 提供"点子王"热点/选题检索服务的可替换 provider。
//
// 管家在用户问热点/选题时，通过 idea_king_api 工具调这里的热点服务，
// 返回带 eid 的选题列表，供管家组织成可点击的热点卡（PromptOptionGroup）。
//
// 可替换：HTTPProvider 接真实热点服务（base_url 可配）；MockProvider 未配置时
// 回退返回带 eid 的结构化选题，保证 demo 恒可跑。
package ideaking

import "context"

// IdeaKingProvider 是热点/选题检索的抽象接口。
type IdeaKingProvider interface {
	// SearchHotTopics 按 query 检索热点选题，count 为期望条数。
	// 返回带 eid 的选题列表 + 热点 markdown 摘要。
	SearchHotTopics(ctx context.Context, query string, count int) (*HotTopicResult, error)
}

// TitleItem 是单个选题（对齐参考 title_data:[{title,content,eid}]）。
// Eid 关联热点 ID：用户点击热点卡选项后回传 selectedEid，据此定位热点详情。
type TitleItem struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Eid     string `json:"eid"`
}

// HotTopicResult 是热点检索结果。
// HotspotData 是热点 markdown 摘要（前端 text-delta 前置展示）；
// TitleData 是可选选题列表（渲染成 PromptOptionGroup 卡的 promptItems）。
type HotTopicResult struct {
	HotspotData string      `json:"hotspot_data,omitempty"`
	TitleData   []TitleItem `json:"title_data"`
}
