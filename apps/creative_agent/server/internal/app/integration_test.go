//go:build integration

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/app"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/service"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx/suite"
	"golang.org/x/crypto/bcrypt"
)

type IntegrationSuite struct {
	suite.Suite
	application *app.App
	auth        *service.AuthService
}

func (s *IntegrationSuite) SetupSuite() {
	// go test's CWD is internal/app/, while config.Load merges secrets.json using relative paths,
	// so switch to the repository root first to ensure config/config.json and config/secrets.json are readable.
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	s.Require().NoError(os.Chdir(repoRoot))

	cfg := config.Load("config/config.json")
	cfg.Redis.DB = 1 // separate db, avoiding pollution of the development cache
	application, err := app.New(cfg)
	s.Require().NoError(err)
	s.Require().NotNil(application.Redis)
	s.application = application
	s.auth = service.NewAuthService(cfg.JWT.Secret)
}

func (s *IntegrationSuite) SetupTest() {
	s.Require().NoError(s.application.DB.Exec("TRUNCATE TABLE users").Error)
	s.Require().NoError(s.application.Redis.FlushDB(context.Background()).Err())
}

func (s *IntegrationSuite) do(method, path, token string, body interface{}) *httptest.ResponseRecorder {
	var buf *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		s.Require().NoError(err)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.application.TestHandler().ServeHTTP(w, req)
	return w
}

func (s *IntegrationSuite) TestRegisterPersistsHashedUser() {
	w := s.do(http.MethodPost, "/api/v1/register", "", map[string]string{
		"username": "alice", "password": "secret123", "email": "alice@example.com",
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)

	var u model.User
	s.Require().NoError(s.application.DB.Where("username = ?", "alice").First(&u).Error)
	testx.NotEqual(s.T(), "secret123", u.Password, "must not store plaintext")
	testx.NoError(s.T(), bcrypt.CompareHashAndPassword([]byte(u.Password), []byte("secret123")))
	testx.Equal(s.T(), "alice@example.com", u.Email)
}

func (s *IntegrationSuite) TestRegisterDuplicateUsernameFails() {
	body := map[string]string{"username": "bob", "password": "secret123", "email": "bob@example.com"}
	s.do(http.MethodPost, "/api/v1/register", "", body)
	w := s.do(http.MethodPost, "/api/v1/register", "", body)
	testx.Equal(s.T(), http.StatusBadRequest, w.Code)
}

func (s *IntegrationSuite) TestLoginIssuesTokenWithJTI() {
	s.do(http.MethodPost, "/api/v1/register", "", map[string]string{
		"username": "carol", "password": "secret123", "email": "carol@example.com",
	})
	w := s.do(http.MethodPost, "/api/v1/login", "", map[string]string{
		"username": "carol", "password": "secret123",
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)

	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(w.Body.Bytes(), &resp))
	s.Require().NotEmpty(resp.Data.Token)

	claims, err := s.auth.ParseToken(resp.Data.Token)
	s.Require().NoError(err)
	testx.NotEmpty(s.T(), claims.JTI)
}

func (s *IntegrationSuite) TestWrongPasswordIncrementsFailCounter() {
	s.do(http.MethodPost, "/api/v1/register", "", map[string]string{
		"username": "dave", "password": "secret123", "email": "dave@example.com",
	})
	w := s.do(http.MethodPost, "/api/v1/login", "", map[string]string{
		"username": "dave", "password": "wrong",
	})
	testx.Equal(s.T(), http.StatusUnauthorized, w.Code)

	n, err := s.application.Redis.Get(context.Background(), "login:fail:dave").Int()
	s.Require().NoError(err)
	testx.Equal(s.T(), 1, n)
}

func (s *IntegrationSuite) TestFiveFailuresLockAccount() {
	s.do(http.MethodPost, "/api/v1/register", "", map[string]string{
		"username": "eve", "password": "secret123", "email": "eve@example.com",
	})
	for i := 0; i < 5; i++ {
		s.do(http.MethodPost, "/api/v1/login", "", map[string]string{"username": "eve", "password": "wrong"})
	}
	// The 6th attempt should be locked
	w := s.do(http.MethodPost, "/api/v1/login", "", map[string]string{"username": "eve", "password": "wrong"})
	testx.Equal(s.T(), http.StatusTooManyRequests, w.Code)
}

func (s *IntegrationSuite) TestSuccessClearsFailCounter() {
	s.do(http.MethodPost, "/api/v1/register", "", map[string]string{
		"username": "frank", "password": "secret123", "email": "frank@example.com",
	})
	s.do(http.MethodPost, "/api/v1/login", "", map[string]string{"username": "frank", "password": "wrong"})
	s.do(http.MethodPost, "/api/v1/login", "", map[string]string{"username": "frank", "password": "secret123"})

	exists, err := s.application.Redis.Exists(context.Background(), "login:fail:frank").Result()
	s.Require().NoError(err)
	testx.Equal(s.T(), int64(0), exists)
}

func (s *IntegrationSuite) TestProtectedRouteAcceptsToken() {
	s.do(http.MethodPost, "/api/v1/register", "", map[string]string{
		"username": "grace", "password": "secret123", "email": "grace@example.com",
	})
	lw := s.do(http.MethodPost, "/api/v1/login", "", map[string]string{"username": "grace", "password": "secret123"})
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(lw.Body.Bytes(), &resp))

	w := s.do(http.MethodGet, "/api/v1/example", resp.Data.Token, nil)
	testx.Equal(s.T(), http.StatusOK, w.Code)
}

func (s *IntegrationSuite) TestLogoutRevokesToken() {
	s.do(http.MethodPost, "/api/v1/register", "", map[string]string{
		"username": "heidi", "password": "secret123", "email": "heidi@example.com",
	})
	lw := s.do(http.MethodPost, "/api/v1/login", "", map[string]string{"username": "heidi", "password": "secret123"})
	var resp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(lw.Body.Bytes(), &resp))
	token := resp.Data.Token

	out := s.do(http.MethodPost, "/api/v1/logout", token, nil)
	testx.Equal(s.T(), http.StatusOK, out.Code)

	// Accessing a protected endpoint after blacklisting should 401
	w := s.do(http.MethodGet, "/api/v1/example", token, nil)
	testx.Equal(s.T(), http.StatusUnauthorized, w.Code)
}

func (s *IntegrationSuite) TestRegisterDuplicateEmailFails() {
	s.do(http.MethodPost, "/api/v1/register", "", map[string]string{
		"username": "irene", "password": "secret123", "email": "irene@example.com",
	})
	w := s.do(http.MethodPost, "/api/v1/register", "", map[string]string{
		"username": "judy", "password": "secret123", "email": "irene@example.com",
	})
	testx.Equal(s.T(), http.StatusBadRequest, w.Code)
}

func TestIntegrationSuite(t *testing.T) {
	suite.Run(t, new(IntegrationSuite))
}
