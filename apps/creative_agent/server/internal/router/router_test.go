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
		UserSvc:   nil, // ping / 无 token 401 用例不触达 user 路由
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

// TestCallbackRouteExemptFromJWT 验证回调路由豁免 JWT（服务端调用，无用户 token 也应可达）。
func (s *RouterSuite) TestCallbackRouteExemptFromJWT() {
	// 回调依赖：最小桩实现（GetWaitingBySession 恒返回 not found → 幂等跳过）。
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

	// 无 Authorization 头调用回调，不应 401（按"无 Waiting 任务"幂等跳过返回 200）。
	req := httptest.NewRequest(http.MethodPost, "/aigc/callback/task_detail",
		strings.NewReader(`{"task_id":1,"project_id":"p1","user_id":1}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusOK, w.Code)
}

// stubTaskRunRepo 是最小 TaskRunRepository 桩：所有查询返回 not found（回调幂等跳过路径）。
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

// stubTaskStepRepo 是最小 TaskStepRepository 桩。
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

// TestConversationRouteRequiresJWT 验证对话路由强制鉴权：无 token 一律 401。
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
			"%s /aigc/conversation 无 token 应 401", method)
	}
}

// TestConversationRouteAcceptsJWT 验证对话路由带有效 token 可达（user_id 注入 ctx）。
func (s *RouterSuite) TestConversationRouteAcceptsJWT() {
	r := s.setupRouter()

	auth := service.NewAuthService("test-secret")
	token, err := auth.GenerateToken("alice", 7)
	s.Require().NoError(err)

	// 无依赖（conversationHandler 的 flow 无法执行），只验证 401 不出现（即通过鉴权）。
	// 这里直接打一个不存在的 business 依赖会 500/流错误，但绝不应是 401。
	req := httptest.NewRequest(http.MethodGet, "/aigc/conversation?query=hi&sessionId=s1&agentId=a1&userId=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	testx.NotEqual(s.T(), http.StatusUnauthorized, w.Code,
		"带有效 token 不应 401")
}

// TestConversationRouteAcceptsQueryToken 验证 JWT 中间件支持 query 参数 ?token= 鉴权
// （EventSource 无法带 Authorization 头，前端用一次性 query token）。
func (s *RouterSuite) TestConversationRouteAcceptsQueryToken() {
	r := s.setupRouter()

	auth := service.NewAuthService("test-secret")
	token, err := auth.GenerateToken("alice", 7)
	s.Require().NoError(err)

	// GET 通过 ?token= 携带 JWT，不应 401（通过鉴权进入 flow）。
	req := httptest.NewRequest(http.MethodGet,
		"/aigc/conversation?query=hi&sessionId=s1&agentId=a1&token="+token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	testx.NotEqual(s.T(), http.StatusUnauthorized, w.Code,
		"带 query token 不应 401")
}

// TestConversationRouteRejectsBadQueryToken 验证非法 query token 被拒绝。
func (s *RouterSuite) TestConversationRouteRejectsBadQueryToken() {
	r := s.setupRouter()

	req := httptest.NewRequest(http.MethodGet,
		"/aigc/conversation?query=hi&sessionId=s1&agentId=a1&token=invalid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusUnauthorized, w.Code,
		"非法 query token 应 401")
}

func TestRouterSuite(t *testing.T) {
	suite.Run(t, new(RouterSuite))
}
