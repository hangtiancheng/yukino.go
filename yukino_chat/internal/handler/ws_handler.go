package handler

import (
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/service"
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/util"

	"github.com/hangtiancheng/yukino.go/yukino_http"
)

// WsLogin authenticates the handshake before upgrading. The identity always
// comes from the token: trusting a client-supplied id would let anyone who
// knows a uuid evict that user's socket in register() and receive their
// messages. Browsers cannot set headers on a websocket handshake, so the
// token rides in the query string.
func WsLogin(ctx *yukino_http.Context, next func()) {
	claims, err := util.ParseToken(ctx.Query("token"), config.Get().Auth.JwtSecret)
	if err != nil {
		JsonStatus(ctx, 401, "invalid or expired token")
		return
	}
	if clientId := ctx.Query("client_id"); clientId != "" && clientId != claims.Uuid {
		JsonStatus(ctx, 403, "client_id does not match the token")
		return
	}
	ws, err := ctx.Upgrade(&yukino_http.UpgradeOptions{
		ReadBufferSize:  2048,
		WriteBufferSize: 2048,
	})
	if err != nil {
		return
	}
	service.NewClientInit(ws, claims.Uuid)
}

// WsLogout disconnects the caller's own socket. owner_id is accepted for
// backwards compatibility but must match the token, so one user cannot force
// another offline.
func WsLogout(ctx *yukino_http.Context, next func()) {
	var req struct {
		OwnerId string `json:"owner_id"`
	}
	if err := ctx.BindJSON(&req); err != nil {
		JsonBack(ctx, "invalid request body", -1, nil)
		return
	}
	uuid, _ := ctx.State["uuid"].(string)
	if req.OwnerId != "" && req.OwnerId != uuid {
		JsonStatus(ctx, 403, "owner_id does not match the token")
		return
	}
	msg, ret := service.ClientLogout(uuid)
	JsonBack(ctx, msg, ret, nil)
}
