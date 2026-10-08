package retriever

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// stubRetriever returns a canned result/error pair.
type stubRetriever struct {
	docs []*schema.Document
	err  error
}

func (s *stubRetriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	return s.docs, s.err
}

func TestEmptyTolerantRetriever(t *testing.T) {
	ctx := context.Background()

	// The Eino Milvus retriever surfaces an empty collection as this error;
	// it must degrade to an empty document list.
	empty := &emptyTolerantRetriever{inner: &stubRetriever{
		err: errors.New("[milvus retriever] no results found"),
	}}
	docs, err := empty.Retrieve(ctx, "q")
	if err != nil {
		t.Fatalf("want nil error on empty collection, got %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("want empty docs, got %d", len(docs))
	}

	// Any other error must pass through untouched.
	boom := errors.New("connection refused")
	passthrough := &emptyTolerantRetriever{inner: &stubRetriever{err: boom}}
	if _, err := passthrough.Retrieve(ctx, "q"); !errors.Is(err, boom) {
		t.Errorf("want pass-through error, got %v", err)
	}

	// Successful results pass through.
	want := []*schema.Document{{ID: "1", Content: "c"}}
	ok := &emptyTolerantRetriever{inner: &stubRetriever{docs: want}}
	got, err := ok.Retrieve(ctx, "q")
	if err != nil || len(got) != 1 || got[0].ID != "1" {
		t.Errorf("want pass-through docs, got %v (err %v)", got, err)
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
