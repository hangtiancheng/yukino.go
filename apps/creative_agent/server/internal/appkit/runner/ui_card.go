package appkitrunner

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/sse"
)

// HotCardData 是管家产出热点卡的透传数据（对齐参考 task_arguments：
// {card_type:"热点及选题卡", hotspot_data, title_data:[{title,content,eid}]}）。
// strategy 把外部热点服务结果组织成它，经 AgentResult.Ext["hot_card"] 透传给 runner，
// runner 渲染成 ui-block-start/ui-block-complete 帧。
type HotCardData struct {
	CardType    string               `json:"card_type"`
	Label       string               `json:"label,omitempty"`
	HotspotData string               `json:"hotspot_data,omitempty"`
	TitleData   []ideaking.TitleItem `json:"title_data"`
	// PromptItems 直接指定选项（可选）：创作功能卡等服务端预定义卡用，
	// 可带 imageUrl（image-text 布局）。有值时优先于 TitleData 转换。
	PromptItems []PromptItem `json:"prompt_items,omitempty"`
	// Layout 是 PromptOptionGroup 的布局：text（纯文字，默认）/ image-text（图标+文字）。
	// 创作功能选择卡用 image-text。
	Layout string `json:"layout,omitempty"`
}

// PromptItem 是热点卡单个选项（对齐参考 PromptItem{title,text,index,eid,imageUrl}）。
type PromptItem struct {
	Title    string `json:"title"`
	Text     string `json:"text"`
	Index    int    `json:"index"`
	Eid      string `json:"eid,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"`
	// Pipeline 是选项关联的产线（创作功能卡用：AI成片→mix_video / AI笔记图→article）。
	// 前端点卡时随 submit_clarification 回传，服务端据此进入对应产线创作。
	Pipeline string `json:"pipeline,omitempty"`
}

// PromptOptionGroupUIData 是 PromptOptionGroup 组件的 uiData（对齐参考）。
type PromptOptionGroupUIData struct {
	Label         string       `json:"label"`
	Layout        string       `json:"layout"`
	Direction     string       `json:"direction"`
	SelectedIndex int          `json:"selectedIndex"`
	PromptItems   []PromptItem `json:"promptItems"`
}

// hotCardLabel 是热点卡的固定引导文案（对齐参考 conversation1 抓包）。
const hotCardLabel = "为您找到了几个热点选题，请挑选一个试试呢～"

// BuildHotIssueFrames 把热点卡数据渲染成 SSE 帧序列：ui-block-start → ui-block-complete。
// ui-block-start 的 UIMeta 用 flex + Markdown + PromptOptionGroup 模板（对齐参考抓包）；
// UIData 填充实际选项（promptItems 带 eid）。
func BuildHotIssueFrames(serverMsgID string, card *HotCardData) []SSEFrame {
	label := card.Label
	if label == "" {
		label = hotCardLabel
	}

	items := make([]PromptItem, 0, len(card.TitleData))
	if len(card.PromptItems) > 0 {
		// 服务端预定义选项（创作功能卡等）：直接用（可带 imageUrl）
		for i, pi := range card.PromptItems {
			if pi.Index == 0 && i > 0 {
				pi.Index = i
			}
			items = append(items, pi)
		}
	} else {
		for i, t := range card.TitleData {
			items = append(items, PromptItem{
				Title: t.Title,
				Text:  t.Content,
				Index: i,
				Eid:   t.Eid,
			})
		}
	}

	layout := card.Layout
	if layout == "" {
		layout = "text"
	}

	uiData := PromptOptionGroupUIData{
		Label:         label,
		Layout:        layout,
		Direction:     "vertical",
		SelectedIndex: -1,
		PromptItems:   items,
	}

	// 对齐参考 buildBlockUIMeta：flex 布局 + Markdown(label) + PromptOptionGroup 模板变量。
	uiMeta := map[string]any{
		"component": map[string]any{
			"type":  "flex",
			"props": map[string]any{"direction": "column", "gap": "15px"},
			"children": []map[string]any{
				{"type": "Markdown", "props": map[string]string{"content": "{{label}}"}},
				{"type": "PromptOptionGroup", "props": map[string]string{
					"layout":        "{{layout}}",
					"direction":     "{{direction}}",
					"promptItems":   "{{promptItems}}",
					"selectedIndex": "{{selectedIndex}}",
				}},
			},
		},
	}

	return []SSEFrame{
		{
			Type:        sse.EventUIBlockStart,
			MsgID:       serverMsgID,
			BlockID:     "block_" + serverMsgID,
			UIMeta:      uiMeta,
			UIData:      uiData,
			Interactive: true,
		},
		{
			Type:    sse.EventUIBlockComplete,
			MsgID:   serverMsgID,
			BlockID: "block_" + serverMsgID,
		},
	}
}
