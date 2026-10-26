package model

import "encoding/json"

// ChatText 是 cg_message_record.chat_text 列中存储的 JSON 结构。
type ChatText struct {
	PageToken    string             `json:"pageToken"`
	QueryInfo    QueryInfo          `json:"queryInfo"`
	ChatResponse []ChatResponseItem `json:"chatResponse"`
	Ext          ChatTextExt        `json:"ext"`
}

// QueryInfo 是 chat_text 中用户提问部分。
type QueryInfo struct {
	Query    []QueryItem `json:"query"`
	Display  bool        `json:"display"`
	Settings interface{} `json:"settings,omitempty"`
}

// QueryItem 是一条查询元素（文本、图片等）。
type QueryItem struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ShowText string `json:"showText,omitempty"`
}

// ChatResponseItem 是 AI 回复中的一条帧记录。
type ChatResponseItem struct {
	MsgID   string      `json:"msgId"`
	SendID  int64       `json:"sendId"`
	Message interface{} `json:"message"`
	LogID   string      `json:"logId,omitempty"`
	EndTurn bool        `json:"endTurn"`
}

// ChatTextExt 是 chat_text 中的扩展字段。
type ChatTextExt struct {
	RequestType string `json:"requestType,omitempty"`
	EID         string `json:"eid,omitempty"`
}

// NewChatText 创建只含 QueryInfo 的 ChatText（con_reader 建记录时使用）。
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

// ParseChatText 将记录中的 ChatText JSON 反序列化。
func (r *CGMessageRecord) ParseChatText() (*ChatText, error) {
	var ct ChatText
	if err := json.Unmarshal([]byte(r.ChatText), &ct); err != nil {
		return nil, err
	}
	return &ct, nil
}

// SetChatText 将 ChatText 序列化写回记录。
func (r *CGMessageRecord) SetChatText(ct *ChatText) error {
	b, err := json.Marshal(ct)
	if err != nil {
		return err
	}
	r.ChatText = string(b)
	return nil
}
