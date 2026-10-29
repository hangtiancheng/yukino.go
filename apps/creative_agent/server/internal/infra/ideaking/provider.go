// Package ideaking provides replaceable providers for the "idea king" hot-topic retrieval service.
//
// When the user asks about hot topics, the manager calls this hotspot service through the idea_king_api tool,
// which returns a topic list with eids for the manager to organize into a clickable hot-topic card (PromptOptionGroup).
//
// Replaceable: HTTPProvider connects to a real hotspot service (configurable base_url); when unconfigured,
// MockProvider falls back to structured topics with eids, guaranteeing the demo always runs.
package ideaking

import "context"

// IdeaKingProvider is the abstraction for hot-topic retrieval.
type IdeaKingProvider interface {
	// SearchHotTopics retrieves hot topics for the query; count is the desired number.
	// Returns the topic list with eids + a hotspot markdown summary.
	SearchHotTopics(ctx context.Context, query string, count int) (*HotTopicResult, error)
}

// TitleItem is a single topic (aligned with the reference title_data:[{title,content,eid}]).
// Eid is the associated hotspot ID: after the user clicks a hot-card option, selectedEid is echoed to locate the hotspot details.
type TitleItem struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Eid     string `json:"eid"`
}

// HotTopicResult is the hot-topic retrieval result.
// HotspotData is the hotspot markdown summary (shown first via text-delta on the frontend);
// TitleData is the selectable topic list (rendered as the promptItems of the PromptOptionGroup card).
type HotTopicResult struct {
	HotspotData string      `json:"hotspot_data,omitempty"`
	TitleData   []TitleItem `json:"title_data"`
}
