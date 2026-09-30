package auth

import (
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/config"
)

func TestTokenRoundTripAndPasswordHash(t *testing.T) {
	cfg := config.Config{JWTKey: "secret", JWTIssuer: "issuer", JWTSubject: "subject", JWTExpire: time.Hour}
	token, err := NewToken(cfg, 1, "user@example.com")
	if err != nil {
		t.Fatalf("NewToken returned error: %v", err)
	}
	username, ok := ParseToken(cfg, token)
	if !ok || username != "user@example.com" {
		t.Fatalf("ParseToken = %q, %v", username, ok)
	}
	if _, ok := ParseToken(config.Config{JWTKey: "other"}, token); ok {
		t.Fatal("token should not verify with a different key")
	}
	if PasswordHash("pass") != "1a1dc91c907325c69271ddf0c944bc72" {
		t.Fatal("unexpected md5 hash")
	}
}

func TestBearerTokenAndNewID(t *testing.T) {
	if got := BearerToken("Bearer abc", "query"); got != "abc" {
		t.Fatalf("BearerToken = %q", got)
	}
	if got := BearerToken("", "query"); got != "query" {
		t.Fatalf("BearerToken fallback = %q", got)
	}
	id, err := NewID()
	if err != nil {
		t.Fatalf("NewID returned error: %v", err)
	}
	if len(id) != 36 {
		t.Fatalf("id length = %d, want 36", len(id))
	}
}
