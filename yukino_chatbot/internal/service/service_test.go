package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/ai"
	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/code"
	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/store"
	"github.com/hangtiancheng/yukino.go/yukino_chatbot/internal/test_util"
)

func newTestServices(t *testing.T) *Services {
	t.Helper()
	database := fmt.Sprintf("server_service_test_%d", time.Now().UnixNano())
	st, err := store.Open(test_util.MongoURI(), database)
	if err != nil {
		if test_util.IsMongoUnauthorized(err) {
			t.Skipf("MongoDB requires authentication; set MONGO_URI with credentials to run integration tests: %v", err)
		}
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := st.DropDatabase(); err != nil {
			t.Fatalf("DropDatabase returned error: %v", err)
		}
		st.Close()
	})
	cfg := config.Config{JWTKey: "secret", JWTIssuer: "issuer", JWTSubject: "subject", JWTExpire: time.Hour, AIBaseURL: "http://127.0.0.1:1", AIModelName: "test"}
	return New(cfg, st, ai.NewManager(cfg, st))
}

func TestRegisterAndLogin(t *testing.T) {
	srv := newTestServices(t)
	ctx := context.Background()
	token, username, result := srv.Register(ctx, "user@example.com", "pass")
	if result != code.OK || token == "" || username != "user@example.com" {
		t.Fatalf("Register = %q, %q, %d", token, username, result)
	}
	if _, _, result := srv.Register(ctx, "user@example.com", "pass"); result != code.UserExist {
		t.Fatalf("duplicate register code = %d", result)
	}
	if token, result := srv.Login(ctx, "user@example.com", "pass"); result != code.OK || token == "" {
		t.Fatalf("Login = %q, %d", token, result)
	}
	if _, result := srv.Login(ctx, "user@example.com", "bad"); result != code.PasswordError {
		t.Fatalf("bad password code = %d", result)
	}
	if _, result := srv.Login(ctx, "missing", "pass"); result != code.UserNotExist {
		t.Fatalf("missing user code = %d", result)
	}
}

func TestSessionAndHistory(t *testing.T) {
	srv := newTestServices(t)
	ctx := context.Background()
	sessionID, result := srv.CreateSession(ctx, "user", "hello")
	if result != code.OK || sessionID == "" {
		t.Fatalf("CreateSession = %q, %d", sessionID, result)
	}
	if sessions := srv.Sessions(ctx, "user"); len(sessions) != 1 || sessions[0].ID != sessionID {
		t.Fatalf("sessions = %+v", sessions)
	}
	if err := srv.Store.CreateMessage(ctx, sessionID, "user", "hello", true); err != nil {
		t.Fatalf("CreateMessage returned error: %v", err)
	}
	history, result := srv.History(ctx, "user", sessionID)
	if result != code.OK || len(history) != 1 || !history[0].IsUser {
		t.Fatalf("history = %+v, %d", history, result)
	}
}

func TestAnswerRejectsUnsupportedModelType(t *testing.T) {
	srv := newTestServices(t)
	answer, result := srv.Answer(context.Background(), "user", "session", "hello", "unknown")
	if result != code.ModelNotFound || answer != "" {
		t.Fatalf("Answer = %q, %d", answer, result)
	}
}

func TestAnswerAcceptsRAGModelType(t *testing.T) {
	srv := newTestServices(t)
	_, result := srv.Answer(context.Background(), "user", "session", "hello", ai.ModelOpenAIRAG)
	if result == code.ModelNotFound {
		t.Fatalf("RAG model type should be accepted, got ModelNotFound")
	}
}
