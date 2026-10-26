package handler

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/middleware"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// Ping 是健康检查接口，无需认证。
func Ping(c *yukino.Context, _ func()) {
	response.OK(c, yukino.H{"message": "pong"})
}

// Example 是受保护的示例接口，返回当前登录用户名（由 JWT 中间件注入）。
func Example(c *yukino.Context, _ func()) {
	username, _ := c.State[middleware.ContextUsername].(string)
	response.OK(c, yukino.H{
		"message":  "this is a protected endpoint",
		"username": username,
	})
}
