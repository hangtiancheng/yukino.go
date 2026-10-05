package codex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_agent_proxy/internal/upstream"
)

func TestCheckRejectsFailedAndTruncatedSuccessResponses(t *testing.T) {
	for _, data := range []string{
		`{"error":{"message":"secret"}}`,
		`{"object":"response","status":"failed","output":[]}`,
		"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp1\"}}\n\n",
		"event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"secret\"}}}\n\n",
	} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, data) }))
		client := upstream.New(config.Provider{Name: "provider", Protocol: config.OpenAI, BaseURL: up.URL, Model: "model", APIKey: "secret"})
		err := Check(context.Background(), client)
		up.Close()
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("check accepted a failed response or exposed credentials: %v", err)
		}
	}
}
