package handler

import (
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/service"

	"github.com/hangtiancheng/yukino.go/yukino_http"
)

func OpenSession(ctx *yukino_http.Context, next func()) {
	var req struct {
		SendId    string `json:"send_id"`
		ReceiveId string `json:"receive_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, sessionId, ret := service.OpenSession(ctx.Request.Context(), req.SendId, req.ReceiveId)
	JsonBack(ctx, msg, ret, sessionId)
}

func GetUserSessionList(ctx *yukino_http.Context, next func()) {
	var req struct {
		OwnerId string `json:"owner_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.GetUserSessionList(ctx.Request.Context(), req.OwnerId)
	JsonBack(ctx, msg, ret, data)
}

func GetGroupSessionList(ctx *yukino_http.Context, next func()) {
	var req struct {
		OwnerId string `json:"owner_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.GetGroupSessionList(ctx.Request.Context(), req.OwnerId)
	JsonBack(ctx, msg, ret, data)
}

func DeleteSession(ctx *yukino_http.Context, next func()) {
	var req struct {
		OwnerId   string `json:"owner_id"`
		SessionId string `json:"session_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, ret := service.DeleteSession(ctx.Request.Context(), req.OwnerId, req.SessionId)
	JsonBack(ctx, msg, ret, nil)
}

func CheckOpenSessionAllowed(ctx *yukino_http.Context, next func()) {
	var req struct {
		SendId    string `json:"send_id"`
		ReceiveId string `json:"receive_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, allowed, ret := service.CheckOpenSessionAllowed(ctx.Request.Context(), req.SendId, req.ReceiveId)
	JsonBack(ctx, msg, ret, allowed)
}

func MarkSessionRead(ctx *yukino_http.Context, next func()) {
	var req struct {
		OwnerId   string `json:"owner_id"`
		ReceiveId string `json:"receive_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, ret := service.MarkSessionRead(ctx.Request.Context(), req.OwnerId, req.ReceiveId)
	JsonBack(ctx, msg, ret, nil)
}
