package handler

import (
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/service"

	"github.com/hangtiancheng/yukino.go/yukino_http"
)

func GetOnlineUsers(ctx *yukino_http.Context, next func()) {
	users := service.GetOnlineUserList()
	JsonBack(ctx, "success", 0, users)
}

// GetCallers lists the other participants of a call room. The room id for a
// group call is just the group uuid, so membership is checked to keep this
// from becoming a presence probe for arbitrary rooms.
func GetCallers(ctx *yukino_http.Context, next func()) {
	var req struct {
		RoomId string `json:"room_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	if req.RoomId == "" {
		JsonBack(ctx, "room_id is required", -2, nil)
		return
	}
	ownerId, _ := ctx.State["uuid"].(string)
	if !service.CanSeeCallRoom(ctx.Request.Context(), req.RoomId, ownerId) {
		JsonStatus(ctx, 403, "not a participant of this call room")
		return
	}
	JsonBack(ctx, "success", 0, service.GetCallers(req.RoomId, ownerId))
}
