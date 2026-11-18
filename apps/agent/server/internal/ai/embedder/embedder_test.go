package embedder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func TestEmbeddingResponseOrderAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		fail       bool
	}{
		{"reordered", `{"data":[{"index":1,"embedding":[3,4]},{"index":0,"embedding":[1,2]}]}`, false},
		{"missing", `{"data":[{"index":0,"embedding":[1,2]}]}`, true},
		{"duplicate", `{"data":[{"index":0,"embedding":[1,2]},{"index":0,"embedding":[3,4]}]}`, true},
		{"invalid index", `{"data":[{"index":0,"embedding":[1,2]},{"index":2,"embedding":[3,4]}]}`, true},
		{"empty", `{"data":[{"index":0,"embedding":[]},{"index":1,"embedding":[3,4]}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/embeddings" {
					t.Errorf("path: %s", r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["model"] != "embedding-test" || body["encoding_format"] != "float" {
					t.Errorf("body: %v", body)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			eb, err := New(context.Background(), &config.Config{EmbeddingModel: config.EmbeddingConfig{Provider: "openai", APIKey: "test", BaseURL: server.URL, Model: "embedding-test"}})
			if err != nil {
				t.Fatal(err)
			}
			vectors, err := eb.EmbedStrings(context.Background(), []string{"one", "two"})
			if tc.fail {
				if err == nil {
					t.Fatalf("accepted invalid embeddings: %v", vectors)
				}
				return
			}
			if err != nil || len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][0] != 3 {
				t.Fatalf("vectors=%v err=%v", vectors, err)
			}
		})
	}
}
