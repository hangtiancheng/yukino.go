package middleware

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/dao"
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/model"
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/util"

	"github.com/hangtiancheng/yukino.go/yukino_http"
)

// publicPaths lists the POST endpoints reachable without a token.
var publicPaths = map[string]bool{
	"/login":                true,
	"/register":             true,
	"/user/update-password": true,
}

// Auth validates the Authorization header on every POST endpoint except the
// public ones. GET endpoints (websocket upgrades, static files, dashboard)
// stay tokenless, matching the legacy behavior.
func Auth() yukino_http.Middleware {
	return func(ctx *yukino_http.Context, next func()) {
		if ctx.Method != "POST" || publicPaths[ctx.Path] {
			next()
			return
		}
		token := strings.TrimPrefix(ctx.Get("Authorization"), "Bearer ")
		if token == "" {
			unauthorized(ctx, "missing token")
			return
		}
		claims, err := util.ParseToken(token, config.Get().Auth.JwtSecret)
		if err != nil {
			unauthorized(ctx, "invalid or expired token")
			return
		}
		ctx.State["uuid"] = claims.Uuid
		next()
	}
}

// RequireAdmin wraps a handler so only admins can invoke it. The admin flag
// is read fresh from the user cache, so revoking admin takes effect without
// re-issuing tokens.
func RequireAdmin(h yukino_http.Middleware) yukino_http.Middleware {
	return func(ctx *yukino_http.Context, next func()) {
		uuid, _ := ctx.State["uuid"].(string)
		if uuid == "" || !isAdmin(ctx.Request.Context(), uuid) {
			ctx.Status = 200
			ctx.JSON(yukino_http.H{"code": 403, "message": "admin privilege required"})
			return
		}
		h(ctx, next)
	}
}

func isAdmin(ctx context.Context, uuid string) bool {
	if view, err := dao.UserInfoCache.Get(ctx, uuid); err == nil {
		var user model.UserInfo
		if err := json.Unmarshal(view.ByteSlice(), &user); err == nil {
			return user.IsAdmin == 1
		}
	}
	var user model.UserInfo
	if err := dao.ActiveQuery(&user).Where("uuid", uuid).First(ctx, &user); err != nil {
		return false
	}
	return user.IsAdmin == 1
}

func unauthorized(ctx *yukino_http.Context, msg string) {
	ctx.Status = 200
	ctx.JSON(yukino_http.H{"code": 401, "message": msg})
}
