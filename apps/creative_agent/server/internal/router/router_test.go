package router_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/router"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/logger"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx/suite"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"go.uber.org/zap"
)

type RouterSuite struct {
	suite.Suite
	log *zap.Logger
}

func (s *RouterSuite) SetupSuite() {
	s.log = logger.New("error")
}

func (s *RouterSuite) setupRouter() *yukino.Application {
	return router.Setup(router.Dependencies{
		UserSvc:   nil, // ping / no-token 401 cases never reach user routes
		JWTSecret: "test-secret",
		Redis:     nil,
		Logger:    s.log,
	})
}

func (s *RouterSuite) TestPingRoute() {
	r := s.setupRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusOK, w.Code)

	var body struct {
		Code int `json:"code"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &body)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), 0, body.Code)
	testx.Equal(s.T(), "pong", body.Data.Message)
}

func (s *RouterSuite) TestProtectedRouteRejectedWithoutAuth() {
	r := s.setupRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/example", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusUnauthorized, w.Code)

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &body)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), 401, body.Code)
	testx.Equal(s.T(), "missing token", body.Message)
}

// TestCallbackRouteExemptFromJWT verifies that callback routes are JWT-exempt (server-to-server; reachable without a user token).
func (s *RouterSuite) TestCallbackRouteExemptFromJWT() {
	// Callback dependencies: minimal stubs (GetWaitingBySession always returns not found → idempotent skip).
	runRepo := &stubTaskRunRepo{}
	stepRepo := &stubTaskStepRepo{}
	r := router.Setup(router.Dependencies{
		UserSvc:      nil,
		JWTSecret:    "test-secret",
		Redis:        nil,
		Logger:       s.log,
		TaskRunRepo:  runRepo,
		TaskStepRepo: stepRepo,
	})

	// Calling the callback without an Authorization header should not 401 (idempotent skip returns 200 as "no Waiting task").
	req := httptest.NewRequest(http.MethodPost, "/aigc/callback/task_detail",
		strings.NewReader(`{"task_id":1,"project_id":"p1","user_id":1}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusOK, w.Code)
}

// stubTaskRunRepo is a minimal TaskRunRepository stub: all queries return not found (the callback idempotent-skip path).
type stubTaskRunRepo struct{}

func (*stubTaskRunRepo) Insert(ctx context.Context, r *model.CGTaskRun) error { return nil }
func (*stubTaskRunRepo) GetByTaskRunID(ctx context.Context, _ int64) (*model.CGTaskRun, error) {
	return nil, repository.ErrTaskRunNotFound
}
func (*stubTaskRunRepo) GetByMsgID(ctx context.Context, _ string) (*model.CGTaskRun, error) {
	return nil, repository.ErrTaskRunNotFound
}
func (*stubTaskRunRepo) GetWaitingBySession(ctx context.Context, _ string, _ int64) (*model.CGTaskRun, error) {
	return nil, repository.ErrTaskRunNotFound
}
func (*stubTaskRunRepo) UpdateCAS(ctx context.Context, _ int64, _ int32, _ map[string]any) (bool, error) {
	return false, nil
}
func (*stubTaskRunRepo) UpdateByTaskRunID(ctx context.Context, _ int64, _ map[string]any) error {
	return nil
}
func (*stubTaskRunRepo) MarkTerminal(ctx context.Context, _ int64, _ int32, _ string) error {
	return nil
}

// stubTaskStepRepo is a minimal TaskStepRepository stub.
type stubTaskStepRepo struct{}

func (*stubTaskStepRepo) Insert(ctx context.Context, s *model.CGTaskStep) error { return nil }
func (*stubTaskStepRepo) GetByTaskStepID(ctx context.Context, _ int64) (*model.CGTaskStep, error) {
	return nil, repository.ErrTaskStepNotFound
}
func (*stubTaskStepRepo) GetByTaskRunID(ctx context.Context, _ int64) ([]model.CGTaskStep, error) {
	return nil, nil
}
func (*stubTaskStepRepo) UpdateByTaskStepID(ctx context.Context, _ int64, _ map[string]any) error {
	return nil
}
func (*stubTaskStepRepo) GetNewestFinishedOutput(ctx context.Context, _ int64) (string, error) {
	return "", nil
}

// TestConversationRouteRequiresJWT verifies that conversation routes require auth: no token always yields 401.
func (s *RouterSuite) TestConversationRouteRequiresJWT() {
	r := s.setupRouter()

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/aigc/conversation?query=hi&sessionId=s1&agentId=a1", nil)
		if method == http.MethodPost {
			req.Body = io.NopCloser(strings.NewReader(`{"query":"hi"}`))
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		testx.Equal(s.T(), http.StatusUnauthorized, w.Code,
			"%s /aigc/conversation without a token should 401", method)
	}
}

// TestConversationRouteAcceptsJWT verifies that conversation routes are reachable with a valid token (user_id injected into ctx).
func (s *RouterSuite) TestConversationRouteAcceptsJWT() {
	r := s.setupRouter()

	auth := service.NewAuthService("test-secret")
	token, err := auth.GenerateToken("alice", 7)
	s.Require().NoError(err)

	// No dependencies (the conversationHandler flow cannot execute); only verify that 401 does not appear (i.e. auth passed).
	// Hitting a missing business dependency here may 500/stream-error, but must never be 401.
	req := httptest.NewRequest(http.MethodGet, "/aigc/conversation?query=hi&sessionId=s1&agentId=a1&userId=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	testx.NotEqual(s.T(), http.StatusUnauthorized, w.Code,
		"a valid token should not 401")
}

// TestConversationRouteAcceptsQueryToken verifies that the JWT middleware supports ?token= query-parameter auth
// (EventSource cannot send an Authorization header, so the frontend uses a one-off query token).
func (s *RouterSuite) TestConversationRouteAcceptsQueryToken() {
	r := s.setupRouter()

	auth := service.NewAuthService("test-secret")
	token, err := auth.GenerateToken("alice", 7)
	s.Require().NoError(err)

	// GET carries the JWT via ?token=; it should not 401 (auth passes and the flow is entered).
	req := httptest.NewRequest(http.MethodGet,
		"/aigc/conversation?query=hi&sessionId=s1&agentId=a1&token="+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	testx.NotEqual(s.T(), http.StatusUnauthorized, w.Code,
		"a query token should not 401")
}

// TestConversationRouteRejectsBadQueryToken verifies that an invalid query token is rejected.
func (s *RouterSuite) TestConversationRouteRejectsBadQueryToken() {
	r := s.setupRouter()

	req := httptest.NewRequest(http.MethodGet,
		"/aigc/conversation?query=hi&sessionId=s1&agentId=a1&token=invalid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusUnauthorized, w.Code,
		"an invalid query token should 401")
}

func TestRouterSuite(t *testing.T) {
	suite.Run(t, new(RouterSuite))
}
