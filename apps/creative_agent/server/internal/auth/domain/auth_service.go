package domain

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// AuthService 负责 JWT 的签发与解析。
//
// 说明：JWT 属应用层关切，但 Claims 需与 middleware 共享（blacklist 检查依赖 JTI），
// 故按 DDD 计划将 AuthService 放在 domain（纯算法，无 IO）。
type AuthService struct {
	secret []byte
	ttl    time.Duration
}

// Claims 是 JWT 中携带的自定义声明。JTI 用于 token 黑名单。
type Claims struct {
	Username string `json:"username"`
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

// GenerateToken 为指定用户签发带 jti 的 JWT。
func (s *AuthService) GenerateToken(username string) (string, error) {
	now := time.Now()
	claims := Claims{
		Username: username,
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
