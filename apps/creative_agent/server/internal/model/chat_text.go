package model

import "encoding/json"

// ChatText is the JSON structure stored in the cg_message_record.chat_text column.
type ChatText struct {
	PageToken    string             `json:"pageToken"`
	QueryInfo    QueryInfo          `json:"queryInfo"`
	ChatResponse []ChatResponseItem `json:"chatResponse"`
	Ext          ChatTextExt        `json:"ext"`
}

// QueryInfo is the user-question part of chat_text.
type QueryInfo struct {
	Query    []QueryItem `json:"query"`
	Display  bool        `json:"display"`
	Settings interface{} `json:"settings,omitempty"`
}

// QueryItem is one query element (text, image, etc.).
type QueryItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ShowText string `json:"showText,omitempty"`
}

// ChatResponseItem is one frame record in the AI reply.
type ChatResponseItem struct {
	MsgID   string      `json:"msgId"`
	SendID  int64       `json:"sendId"`
	Message interface{} `json:"message"`
	LogID   string      `json:"logId,omitempty"`
	EndTurn bool        `json:"endTurn"`
}

// ChatTextExt holds the extension fields of chat_text.
type ChatTextExt struct {
	RequestType string `json:"requestType,omitempty"`
	EID         string `json:"eid,omitempty"`
}

// NewChatText creates a ChatText containing only QueryInfo (used by con_reader when creating the record).
func NewChatText(query string) *ChatText {
	return &ChatText{
		QueryInfo: QueryInfo{
			Query:   []QueryItem{{Type: "text", Text: query, ShowText: query}},
			Display: true,
		},
		ChatResponse: []ChatResponseItem{},
		Ext:          ChatTextExt{},
	}
}

// ParseChatText deserializes the ChatText JSON from the record.
func (r *CGMessageRecord) ParseChatText() (*ChatText, error) {
	var ct ChatText
	if err := json.Unmarshal([]byte(r.ChatText), &ct); err != nil {
		return nil, err
	}
	return &ct, nil
}

// SetChatText serializes ChatText back into the record.
func (r *CGMessageRecord) SetChatText(ct *ChatText) error {
	b, err := json.Marshal(ct)
	if err != nil {
		return err
	}
	r.ChatText = string(b)
	return nil
}
