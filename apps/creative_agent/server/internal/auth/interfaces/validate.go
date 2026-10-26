package interfaces

import (
	"fmt"
	"net/mail"
	"strings"
)

// validateRegisterRequest 校验注册请求（替代 gin binding tag：required/min/max/email）。
func validateRegisterRequest(req *registerRequest) error {
	username := strings.TrimSpace(req.Username)
	if len(username) < 3 || len(username) > 64 {
		return fmt.Errorf("username length must be 3-64")
	}
	if len(req.Password) < 6 {
		return fmt.Errorf("password length must be at least 6")
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		return fmt.Errorf("invalid email: %w", err)
	}
	return nil
}

// validateLoginRequest 校验登录请求（替代 gin binding tag：required）。
func validateLoginRequest(req *loginRequest) error {
	if strings.TrimSpace(req.Username) == "" {
		return fmt.Errorf("username is required")
	}
	if req.Password == "" {
		return fmt.Errorf("password is required")
	}
	return nil
}
