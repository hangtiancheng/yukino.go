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

func TestEmbeddingBatchesProviderRequests(t *testing.T) {
	var sizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		sizes = append(sizes, len(body.Input))
		var data []map[string]any
		for n := len(body.Input) - 1; n >= 0; n-- {
			var value int
			fmt.Sscanf(body.Input[n], "%d", &value)
			data = append(data, map[string]any{"index": n, "embedding": []float64{float64(value)}})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	eb, err := New(context.Background(), &config.Config{EmbeddingModel: config.EmbeddingConfig{Provider: "openai", BaseURL: server.URL, APIKey: "test", Model: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, 43)
	for n := range texts {
		texts[n] = fmt.Sprint(n)
	}
	vectors, err := eb.EmbedStrings(context.Background(), texts)
	if err != nil || len(vectors) != 43 || fmt.Sprint(sizes) != "[20 20 3]" {
		t.Fatalf("batches=%v vectors=%d error=%v", sizes, len(vectors), err)
	}
	for n, v := range vectors {
		if v[0] != float64(n) {
			t.Fatalf("output order at %d: %v", n, v)
		}
	}
}
