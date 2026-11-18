package retriever

import (
	"context"
	"errors"
	"testing"

	milvusclient "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

func TestSearchDocuments(t *testing.T) {
	docs, err := searchDocuments([]milvusclient.SearchResult{{ResultCount: 0}})
	if err != nil || len(docs) != 0 {
		t.Fatalf("empty results: %v %v", docs, err)
	}
	boom := errors.New("connection refused")
	if _, err = searchDocuments([]milvusclient.SearchResult{{Err: boom}}); !errors.Is(err, boom) {
		t.Fatalf("error lost: %v", err)
	}
	results := []milvusclient.SearchResult{{ResultCount: 1, IDs: entity.NewColumnVarChar("id", []string{"doc1"}), Fields: milvusclient.ResultSet{entity.NewColumnVarChar("content", []string{"hello"}), entity.NewColumnJSONBytes("metadata", [][]byte{[]byte(`{"title":"Guide"}`)})}}}
	docs, err = searchDocuments(results)
	if err != nil || len(docs) != 1 || docs[0].ID != "doc1" || docs[0].Content != "hello" || docs[0].MetaData["title"] != "Guide" {
		t.Fatalf("documents: %+v %v", docs, err)
	}
}

func TestFloatVectorConverter(t *testing.T) {
	vecs, err := floatVectorConverter(context.Background(), [][]float64{{0.5, -1.25}, {2.0}})
	if err != nil {
		t.Fatalf("floatVectorConverter: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("want 2 vectors, got %d", len(vecs))
	}
	fv, ok := vecs[0].(entity.FloatVector)
	if !ok {
		t.Fatalf("want entity.FloatVector, got %T", vecs[0])
	}
	if len(fv) != 2 || fv[0] != 0.5 || fv[1] != -1.25 {
		t.Errorf("vector = %v, want [0.5 -1.25]", fv)
	}
}
