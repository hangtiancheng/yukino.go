package service

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// AuthService handles JWT issuance and parsing.
type AuthService struct {
	secret []byte
	ttl    time.Duration
}

// Claims are the custom claims carried in the JWT. UserID is used for cross-service identity propagation.
type Claims struct {
	Username string `json:"username"`
	UserID   int64  `json:"user_id"`
	JTI      string `json:"jti"`
	jwt.RegisteredClaims
}

// NewAuthService creates an AuthService; tokens default to a 24-hour validity.
func NewAuthService(secret string) *AuthService {
	return &AuthService{
		secret: []byte(secret),
		ttl:    24 * time.Hour,
	}
}

// GenerateToken issues a JWT with jti + user_id for the given user.
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

// ParseToken parses and validates the token, returning the Claims.
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
