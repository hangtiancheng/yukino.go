package handler

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/middleware"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// Ping is the health-check endpoint; no authentication required.
func Ping(c *yukino.Context, _ func()) {
	response.OK(c, yukino.H{"message": "pong"})
}

// Example is a protected sample endpoint returning the current username (injected by the JWT middleware).
func Example(c *yukino.Context, _ func()) {
	username, _ := c.State[middleware.ContextUsername].(string)
	response.OK(c, yukino.H{
		"message":  "this is a protected endpoint",
		"username": username,
	})
}
