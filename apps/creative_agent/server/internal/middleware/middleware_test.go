package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/middleware"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/logger"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx/suite"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

type MiddlewareSuite struct {
	suite.Suite
	engine *yukino.Application
}

func (s *MiddlewareSuite) SetupTest() {
	s.engine = yukino.New()
}

func (s *MiddlewareSuite) TestRequestIDGeneratesIDWhenMissing() {
	s.engine.Use(middleware.RequestID())
	s.engine.Get("/test", func(c *yukino.Context, _ func()) {
		id := c.State[middleware.ContextRequestID]
		testx.NotNil(s.T(), id)
		testx.NotEmpty(s.T(), id)
		c.SetStatus(200)
		c.String("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusOK, w.Code)
	testx.NotEmpty(s.T(), w.Header().Get("X-Request-ID"))
}

func (s *MiddlewareSuite) TestRequestIDReusesProvidedID() {
	s.engine.Use(middleware.RequestID())
	s.engine.Get("/test", func(c *yukino.Context, _ func()) {
		testx.Equal(s.T(), "my-custom-id", c.State[middleware.ContextRequestID])
		c.SetStatus(200)
		c.String("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", "my-custom-id")
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusOK, w.Code)
	testx.Equal(s.T(), "my-custom-id", w.Header().Get("X-Request-ID"))
}

func (s *MiddlewareSuite) TestRecoveryReturns500OnPanic() {
	log := logger.New("error")
	s.engine.Use(middleware.Recovery(log))
	s.engine.Get("/panic", func(c *yukino.Context, _ func()) {
		panic("something went wrong")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusInternalServerError, w.Code)
}

func (s *MiddlewareSuite) TestRecoveryDoesNotAffectNormalRequests() {
	log := logger.New("error")
	s.engine.Use(middleware.Recovery(log))
	s.engine.Get("/ok", func(c *yukino.Context, _ func()) {
		c.SetStatus(200)
		c.String("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusOK, w.Code)
}

func (s *MiddlewareSuite) TestJWTRejectsMissingAuthHeader() {
	s.engine.Use(middleware.JWT("test-secret", nil))
	s.engine.Get("/protected", func(c *yukino.Context, _ func()) {
		c.SetStatus(200)
		c.String("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusUnauthorized, w.Code)
}

func (s *MiddlewareSuite) TestJWTRejectsInvalidToken() {
	s.engine.Use(middleware.JWT("test-secret", nil))
	s.engine.Get("/protected", func(c *yukino.Context, _ func()) {
		c.SetStatus(200)
		c.String("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusUnauthorized, w.Code)
}

func (s *MiddlewareSuite) TestJWTAllowsValidToken() {
	auth := service.NewAuthService("test-secret")
	token, err := auth.GenerateToken("alice", 7)
	testx.NoError(s.T(), err)

	s.engine.Use(middleware.JWT("test-secret", nil))
	s.engine.Get("/protected", func(c *yukino.Context, _ func()) {
		testx.Equal(s.T(), "alice", c.State[middleware.ContextUsername])
		testx.Equal(s.T(), int64(7), c.State[middleware.ContextUserID])
		c.SetStatus(200)
		c.String("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusOK, w.Code)
}

// TestJWTAllowsLowercaseBearer 验证 scheme 大小写不敏感（RFC 6750：bearer xxx 也应通过）。
func (s *MiddlewareSuite) TestJWTAllowsLowercaseBearer() {
	auth := service.NewAuthService("test-secret")
	token, err := auth.GenerateToken("alice", 7)
	testx.NoError(s.T(), err)

	s.engine.Use(middleware.JWT("test-secret", nil))
	s.engine.Get("/protected", func(c *yukino.Context, _ func()) {
		testx.Equal(s.T(), int64(7), c.State[middleware.ContextUserID])
		c.SetStatus(200)
		c.String("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "bearer "+token)
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusOK, w.Code)
}

// TestJWTAllowsQueryToken 验证无 Authorization 头时回退 query 参数 ?token= 鉴权。
func (s *MiddlewareSuite) TestJWTAllowsQueryToken() {
	auth := service.NewAuthService("test-secret")
	token, err := auth.GenerateToken("alice", 7)
	testx.NoError(s.T(), err)

	s.engine.Use(middleware.JWT("test-secret", nil))
	s.engine.Get("/protected", func(c *yukino.Context, _ func()) {
		testx.Equal(s.T(), int64(7), c.State[middleware.ContextUserID])
		c.SetStatus(200)
		c.String("ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/protected?token="+token, nil)
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, req)

	testx.Equal(s.T(), http.StatusOK, w.Code)
}

func TestMiddlewareSuite(t *testing.T) {
	suite.Run(t, new(MiddlewareSuite))
}
