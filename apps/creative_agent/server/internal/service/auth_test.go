package service_test

import (
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

func TestGenerateTokenHasJTI(t *testing.T) {
	s := service.NewAuthService("test-secret")
	token, err := s.GenerateToken("alice", 7)
	testx.NoError(t, err)
	testx.NotEmpty(t, token)

	claims, err := s.ParseToken(token)
	testx.NoError(t, err)
	testx.Equal(t, "alice", claims.Username)
	testx.NotEmpty(t, claims.JTI, "token must carry a jti")
}

func TestParseTokenRejectsBadSignature(t *testing.T) {
	signer := service.NewAuthService("secret-a")
	verifier := service.NewAuthService("secret-b")
	token, err := signer.GenerateToken("alice", 7)
	testx.NoError(t, err)

	_, err = verifier.ParseToken(token)
	testx.Error(t, err)
}

// TestGenerateToken_IncludesUserID 验证 token 携带 user_id。
func TestGenerateToken_IncludesUserID(t *testing.T) {
	svc := service.NewAuthService("test-secret")
	token, err := svc.GenerateToken("alice", 7)
	testx.NoError(t, err)

	claims, err := svc.ParseToken(token)
	testx.NoError(t, err)
	testx.Equal(t, int64(7), claims.UserID)
}
