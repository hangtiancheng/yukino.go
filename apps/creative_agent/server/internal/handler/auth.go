package handler

import (
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/middleware"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// AuthHandler handles authentication requests.
type AuthHandler struct {
	user *service.UserService
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(user *service.UserService) *AuthHandler {
	return &AuthHandler{user: user}
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Register creates a new user.
func (h *AuthHandler) Register(c *yukino.Context, _ func()) {
	var req registerRequest
	if err := c.BindJSON(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}
	if err := validateRegisterRequest(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}
	err := h.user.Register(c.Request.Context(), req.Username, req.Password, req.Email)
	switch {
	case errors.Is(err, service.ErrUserExists):
		response.Fail(c, 400, 40000, err.Error())
	case errors.Is(err, service.ErrEmailExists):
		response.Fail(c, 400, 40000, err.Error())
	case err != nil:
		response.Fail(c, 500, 50000, "register failed")
	default:
		response.OK(c, yukino.H{"message": "registered"})
	}
}

// Login validates the username/password and issues a JWT.
func (h *AuthHandler) Login(c *yukino.Context, _ func()) {
	var req loginRequest
	if err := c.BindJSON(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}
	if err := validateLoginRequest(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}
	token, err := h.user.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUserLocked):
			response.Fail(c, 429, 42900, err.Error())
		case errors.Is(err, service.ErrInvalidCredentials):
			response.Fail(c, 401, 40100, err.Error())
		default:
			response.Fail(c, 500, 50000, "login failed")
		}
		return
	}
	response.OK(c, yukino.H{"token": token})
}

// Logout blacklists the current token. jti/exp are injected into the context by the JWT middleware.
func (h *AuthHandler) Logout(c *yukino.Context, _ func()) {
	jti, _ := c.State[middleware.ContextJTI].(string)
	if jti == "" {
		response.Fail(c, 401, 40100, "invalid token")
		return
	}
	var exp time.Time
	if t, ok := c.State[middleware.ContextExp].(time.Time); ok {
		exp = t
	}
	if err := h.user.LogoutByJTI(c.Request.Context(), jti, exp); err != nil {
		response.Fail(c, 500, 50000, "logout failed")
		return
	}
	response.OK(c, yukino.H{"message": "logged out"})
}
