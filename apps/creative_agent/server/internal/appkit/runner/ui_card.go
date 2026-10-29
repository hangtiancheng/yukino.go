package appkitrunner

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/sse"
)

// HotCardData is the pass-through data of the hot-topic card produced by the manager (aligned with the reference task_arguments:
// {card_type:"热点及选题卡", hotspot_data, title_data:[{title,content,eid}]}).
// The strategy organizes external hotspot-service results into it and passes it to the runner via AgentResult.Ext["hot_card"];
// the runner renders it into ui-block-start/ui-block-complete frames.
type HotCardData struct {
	CardType    string               `json:"card_type"`
	Label       string               `json:"label,omitempty"`
	HotspotData string               `json:"hotspot_data,omitempty"`
	TitleData   []ideaking.TitleItem `json:"title_data"`
	// PromptItems specifies the options directly (optional): used by server-predefined cards such as the creation-feature card;
	// may carry imageUrl (image-text layout). When set, it takes priority over converting TitleData.
	PromptItems []PromptItem `json:"prompt_items,omitempty"`
	// Layout is the PromptOptionGroup layout: text (plain text, default) / image-text (icon + text).
	// The creation-feature selection card uses image-text.
	Layout string `json:"layout,omitempty"`
}

// PromptItem is a single hot-card option (aligned with the reference PromptItem{title,text,index,eid,imageUrl}).
type PromptItem struct {
	Title    string `json:"title"`
	Text     string `json:"text"`
	Index    int    `json:"index"`
	Eid      string `json:"eid,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"`
	// Pipeline is the pipeline associated with the option (used by the creation-feature card: AI成片→mix_video / AI笔记图→article).
	// The frontend echoes it with submit_clarification on card click; the server enters the corresponding pipeline creation based on it.
	Pipeline string `json:"pipeline,omitempty"`
}

// PromptOptionGroupUIData is the uiData of the PromptOptionGroup component (aligned with the reference).
type PromptOptionGroupUIData struct {
	Label         string       `json:"label"`
	Layout        string       `json:"layout"`
	Direction     string       `json:"direction"`
	SelectedIndex int          `json:"selectedIndex"`
	PromptItems   []PromptItem `json:"promptItems"`
}

// hotCardLabel is the fixed guidance copy of the hot-topic card (aligned with the reference conversation1 capture; product content, kept in Chinese).
const hotCardLabel = "为您找到了几个热点选题，请挑选一个试试呢～"

// BuildHotIssueFrames renders hot-card data into the SSE frame sequence: ui-block-start → ui-block-complete.
// The ui-block-start UIMeta uses the flex + Markdown + PromptOptionGroup template (aligned with the reference capture);
// UIData is filled with the actual options (promptItems carrying eid).
func BuildHotIssueFrames(serverMsgID string, card *HotCardData) []SSEFrame {
	label := card.Label
	if label == "" {
		label = hotCardLabel
	}

	items := make([]PromptItem, 0, len(card.TitleData))
	if len(card.PromptItems) > 0 {
		// Server-predefined options (creation-feature card, etc.): use directly (may carry imageUrl)
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

	// Aligned with the reference buildBlockUIMeta: flex layout + Markdown(label) + PromptOptionGroup template variables.
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
