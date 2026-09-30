package rpc_client

type AIRequest struct {
	Username  string
	SessionID string
	Question  string
	ModelType string
}

type AIResponse struct {
	Answer string
	Code   int
}

type AIStreamChunk struct {
	Content string
}
