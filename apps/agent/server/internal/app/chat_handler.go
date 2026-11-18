package app

import (
	"github.com/hangtiancheng/yukino-code/apps/yukino/conversation"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/agent/chat_pipeline"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/mem"
	"github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"net/http"
)

type chatRequest struct {
	ID       string `json:"id"`
	Question string `json:"question"`
}

func readChat(ctx *yukino_http.Context) (chatRequest, bool) {
	var req chatRequest
	if err := ctx.BindJSON(&req); err != nil {
		ctx.Throw(http.StatusBadRequest, "invalid request body")
		return req, false
	}
	if req.ID == "" || req.Question == "" {
		ctx.Throw(http.StatusBadRequest, "missing id or question")
		return req, false
	}
	return req, true
}
func saveChat(id, question, answer string) {
	memory := mem.Get(id)
	memory.Append(conversation.Message{Role: "user", Content: question})
	memory.Append(conversation.Message{Role: "assistant", Content: answer})
}
func (a *App) handleChat(ctx *yukino_http.Context, next func()) {
	req, ok := readChat(ctx)
	if !ok {
		return
	}
	answer, err := chat_pipeline.Run(ctx.Request.Context(), a.cfg, req.Question, mem.Get(req.ID).All(), nil)
	if err != nil {
		ctx.Throw(http.StatusInternalServerError, structuredErrorMessage(err))
		return
	}
	saveChat(req.ID, req.Question, answer)
	ctx.JSON(yukino_http.H{"message": "OK", "data": yukino_http.H{"answer": answer}})
}
func (a *App) handleChatStream(ctx *yukino_http.Context, next func()) {
	req, ok := readChat(ctx)
	if !ok {
		return
	}
	sse := ctx.SSE()
	sse.JSON("connected", map[string]string{"status": "connected", "client_id": req.ID})
	answer, err := chat_pipeline.Run(ctx.Request.Context(), a.cfg, req.Question, mem.Get(req.ID).All(), func(text string) {
		if text != "" {
			sse.Event("message", text)
		}
	})
	if err != nil {
		sse.Event("error", err.Error())
		return
	}
	saveChat(req.ID, req.Question, answer)
	sse.Event("done", "Stream completed")
}
