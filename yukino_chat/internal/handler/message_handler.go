package handler

import (
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/service"

	"github.com/hangtiancheng/yukino.go/yukino_http"
)

func GetMessageList(ctx *yukino_http.Context, next func()) {
	var req struct {
		SendId    string `json:"send_id"`
		ReceiveId string `json:"receive_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.GetMessageList(ctx.Request.Context(), req.SendId, req.ReceiveId)
	JsonBack(ctx, msg, ret, data)
}

func GetGroupMessageList(ctx *yukino_http.Context, next func()) {
	var req struct {
		GroupId string `json:"group_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	msg, data, ret := service.GetGroupMessageList(ctx.Request.Context(), req.GroupId)
	JsonBack(ctx, msg, ret, data)
}
