package service

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// AuthService 负责 JWT 的签发与解析。
type AuthService struct {
	secret []byte
	ttl    time.Duration
}

// Claims 是 JWT 中携带的自定义声明。UserID 用于跨服务身份透传。
type Claims struct {
	Username string `json:"username"`
	UserID   int64  `json:"user_id"`
	JTI      string `json:"jti"`
	jwt.RegisteredClaims
}

// NewAuthService 创建 AuthService，token 默认有效期 24 小时。
func NewAuthService(secret string) *AuthService {
	return &AuthService{
		secret: []byte(secret),
		ttl:    24 * time.Hour,
	}
}

// GenerateToken 为指定用户签发带 jti + user_id 的 JWT。
func (s *AuthService) GenerateToken(username string, userID int64) (string, error) {
	now := time.Now()
	claims := Claims{
		Username: username,
		UserID:   userID,
		JTI:      uuid.NewString(),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// ParseToken 解析并校验 token，返回 Claims。
func (s *AuthService) ParseToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
