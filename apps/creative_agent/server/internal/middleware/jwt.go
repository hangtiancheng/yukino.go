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

// ctx.State keys for JWT.
const (
	ContextUsername = "username"
	ContextUserID   = "user_id"
	ContextJTI      = "jti"
	ContextExp      = "exp"
)

// jwtClaims matches the fields of service.Claims and is compatible via the JSON serialization protocol.
// UserID is optional: legacy tokens issued by the base-server domain carry no user_id and parse to 0.
type jwtClaims struct {
	Username string `json:"username"`
	UserID   int64  `json:"user_id"`
	JTI      string `json:"jti"`
	jwt.RegisteredClaims
}

// JWT returns a middleware validating the Bearer token.
// Token source: the Authorization: Bearer <token> header takes priority; otherwise it falls back to the ?token=<token> query parameter
// (EventSource cannot send custom headers, so the frontend authenticates with a one-off query token).
// secret is the signing key; rdb is used to query the token blacklist and is skipped when nil (degraded mode).
func JWT(secret string, rdb *redis.Client) yukino.Middleware {
	return func(ctx *yukino.Context, next func()) {
		// Get the token: the Authorization: Bearer <token> header takes priority (scheme is case-insensitive, RFC 6750);
		// otherwise fall back to the token query parameter (EventSource scenario).
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

		// Blacklist check
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
