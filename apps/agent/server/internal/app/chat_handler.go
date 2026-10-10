package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/agent/chat_pipeline"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/log_callback"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/utility/mem"
	"github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

type chatRequest struct {
	ID       string `json:"id"`
	Question string `json:"question"`
}

func (a *App) handleChat(ctx *yukino_http.Context, next func()) {
	var req chatRequest
	if err := ctx.BindJSON(&req); err != nil {
		ctx.Throw(http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ID == "" || req.Question == "" {
		ctx.Throw(http.StatusBadRequest, "missing id or question")
		return
	}

	appCtx := ctx.Request.Context()
	userMsg := &chat_pipeline.UserMessage{
		ID:      req.ID,
		Query:   req.Question,
		History: mem.Get(req.ID).All(),
	}

	runner, err := chat_pipeline.BuildChatAgent(appCtx, a.cfg)
	if err != nil {
		ctx.Throw(http.StatusInternalServerError, structuredErrorMessage(err))
		return
	}

	out, err := runner.Invoke(appCtx, userMsg, compose.WithCallbacks(log_callback.NewHandler(nil)))
	if err != nil {
		ctx.Throw(http.StatusInternalServerError, structuredErrorMessage(err))
		return
	}

	raw := out.Content
	mem.Get(req.ID).Append(schema.UserMessage(req.Question))
	mem.Get(req.ID).Append(schema.AssistantMessage(raw, nil))

	ctx.Status = http.StatusOK
	ctx.JSON(yukino_http.H{
		"message": "OK",
		"data":    yukino_http.H{"answer": raw},
	})
}

func (a *App) handleChatStream(ctx *yukino_http.Context, next func()) {
	var req chatRequest
	if err := ctx.BindJSON(&req); err != nil {
		ctx.Throw(http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ID == "" || req.Question == "" {
		ctx.Throw(http.StatusBadRequest, "missing id or question")
		return
	}

	appCtx := ctx.Request.Context()
	sse := ctx.SSE()
	connectedPayload, _ := json.Marshal(map[string]string{
		"status":    "connected",
		"client_id": req.ID,
	})
	sse.Event("connected", string(connectedPayload))

	userMsg := &chat_pipeline.UserMessage{
		ID:      req.ID,
		Query:   req.Question,
		History: mem.Get(req.ID).All(),
	}

	runner, err := chat_pipeline.BuildChatAgent(appCtx, a.cfg)
	if err != nil {
		sse.Event("error", err.Error())
		return
	}

	sr, err := runner.Stream(appCtx, userMsg, compose.WithCallbacks(log_callback.NewHandler(nil)))
	if err != nil {
		sse.Event("error", err.Error())
		return
	}
	defer sr.Close()

	var fullResponse strings.Builder
	defer func() {
		resp := fullResponse.String()
		if resp != "" {
			mem.Get(req.ID).Append(schema.UserMessage(req.Question))
			mem.Get(req.ID).Append(schema.AssistantMessage(resp, nil))
		}
	}()

	for {
		chunk, err := sr.Recv()
		if errors.Is(err, io.EOF) {
			sse.Event("done", "Stream completed")
			return
		}
		if err != nil {
			sse.Event("error", err.Error())
			return
		}
		fullResponse.WriteString(chunk.Content)
		if chunk.Content != "" {
			sse.Event("message", chunk.Content)
		}
	}
}
