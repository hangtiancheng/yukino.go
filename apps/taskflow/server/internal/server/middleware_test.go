package server

import (
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenAndInternalAccess(t *testing.T) {
	for _, test := range []struct {
		token, header, remote string
		internal              bool
		want                  int
	}{
		{"secret", "", "127.0.0.1:1", false, 401}, {"secret", "Bearer secret", "192.0.2.1:1", false, 200}, {"", "", "192.0.2.1:1", true, 403}, {"", "", "127.0.0.1:1", true, 200},
	} {
		app := yukino.New()
		app.Use(TokenMiddleware(test.token, test.internal))
		app.Get("/", func(c *yukino.Context, _ func()) { c.String("ok") })
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = test.remote
		request.Header.Set("Authorization", test.header)
		response := httptest.NewRecorder()
		app.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("token=%q internal=%v status=%d", test.token, test.internal, response.Code)
		}
	}
}

func TestLargeBodyRejected(t *testing.T) {
	app := yukino.New()
	app.Use(BodyLimitMiddleware())
	app.Post("/", func(c *yukino.Context, _ func()) { c.String("ok") })
	request := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != 413 {
		t.Fatalf("body limit returned %d", response.Code)
	}
}
