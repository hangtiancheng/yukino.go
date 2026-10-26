package middleware

import (
	"errors"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	"github.com/golang-jwt/jwt/v5"
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"github.com/redis/go-redis/v9"
)

// JWT 相关的 ctx.State 键。
const (
	ContextUsername = "username"
	ContextUserID   = "user_id"
	ContextJTI      = "jti"
	ContextExp      = "exp"
)

// jwtClaims 与 service.Claims 字段一致，通过 JSON 序列化协议兼容。
// UserID 可选：base-server 域签发的旧 token 不含 user_id，解析后为 0。
type jwtClaims struct {
	Username string `json:"username"`
	UserID   int64  `json:"user_id"`
	JTI      string `json:"jti"`
	jwt.RegisteredClaims
}

// JWT 返回校验 Bearer token 的中间件。
// token 来源：优先 Authorization: Bearer <token> 头；无则回退 query 参数 ?token=<token>
// （EventSource 无法带自定义头，前端通过一次性 query token 鉴权）。
// secret 为签名密钥；rdb 用于查询 token 黑名单，nil 时跳过（降级）。
func JWT(secret string, rdb *redis.Client) yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		// 取 token：优先 Authorization: Bearer <token> 头（scheme 大小写不敏感，RFC 6750）；
		// 无则回退 query 参数 token（EventSource 场景）。
		tokenStr := ""
		header := ctx.Get("Authorization")
		parts := strings.Fields(header)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			tokenStr = parts[1]
		} else {
			tokenStr = ctx.Query("token")
		}
		if tokenStr == "" {
			response.Fail(ctx, 401, 401, "missing token")
			return
		}

		token, err := jwt.ParseWithClaims(tokenStr, &jwtClaims{}, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(secret), nil
		})
		if err != nil {
			response.Fail(ctx, 401, 401, "invalid or expired token")
			return
		}

		claims, ok := token.Claims.(*jwtClaims)
		if !ok || !token.Valid {
			response.Fail(ctx, 401, 401, "invalid or expired token")
			return
		}

		// 黑名单检查
		if rdb != nil && claims.JTI != "" {
			if n, err := rdb.Exists(ctx.Request.Context(), service.JwtBlacklistKey(claims.JTI)).Result(); err == nil && n > 0 {
				response.Fail(ctx, 401, 401, "token has been revoked")
				return
			}
		}

		ctx.State[ContextUsername] = claims.Username
		ctx.State[ContextUserID] = claims.UserID
		ctx.State[ContextJTI] = claims.JTI
		if claims.ExpiresAt != nil {
			ctx.State[ContextExp] = claims.ExpiresAt.Time
		}
		next()
	}
}
