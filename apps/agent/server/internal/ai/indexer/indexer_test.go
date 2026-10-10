package indexer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/consts"
)

func TestDocumentToRows(t *testing.T) {
	docs := []*schema.Document{
		{
			ID:      "doc-1",
			Content: "hello world",
			MetaData: map[string]any{
				consts.MilvusSourceKey: "/some/absolute/dir/guide.md",
				"title":                "Guide",
			},
		},
	}
	vectors := [][]float64{{0.5, -0.25, 1.0}}

	rows, err := documentToRows(context.Background(), docs, vectors)
	if err != nil {
		t.Fatalf("documentToRows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	row, ok := rows[0].(*docRow)
	if !ok {
		t.Fatalf("want *docRow, got %T", rows[0])
	}

	if row.ID != "doc-1" {
		t.Errorf("id = %q, want doc-1", row.ID)
	}
	if row.Content != "hello world" {
		t.Errorf("content = %q", row.Content)
	}
	wantVec := []float32{0.5, -0.25, 1.0}
	if len(row.Vector) != len(wantVec) {
		t.Fatalf("vector len = %d, want %d", len(row.Vector), len(wantVec))
	}
	for i := range wantVec {
		if row.Vector[i] != wantVec[i] {
			t.Errorf("vector[%d] = %v, want %v", i, row.Vector[i], wantVec[i])
		}
	}

	var meta map[string]any
	if err := json.Unmarshal(row.Metadata, &meta); err != nil {
		t.Fatalf("metadata is not valid JSON: %v", err)
	}
	if got := meta[consts.MilvusSourceKey]; got != "guide.md" {
		t.Errorf("_source = %v, want basename guide.md", got)
	}
	if got := meta["title"]; got != "Guide" {
		t.Errorf("title = %v, want Guide", got)
	}
}

func TestDocumentToRowsTruncatesContent(t *testing.T) {
	long := strings.Repeat("a", consts.MaxContentLength+100)
	docs := []*schema.Document{{ID: "doc-2", Content: long, MetaData: map[string]any{}}}
	vectors := [][]float64{{0.1}}

	rows, err := documentToRows(context.Background(), docs, vectors)
	if err != nil {
		t.Fatalf("documentToRows: %v", err)
	}
	row := rows[0].(*docRow)
	if len(row.Content) != consts.MaxContentLength {
		t.Errorf("content len = %d, want %d", len(row.Content), consts.MaxContentLength)
	}
}

func TestDocumentToRowsErrors(t *testing.T) {
	ctx := context.Background()

	if _, err := documentToRows(ctx, []*schema.Document{{ID: "a"}}, nil); err == nil {
		t.Error("want error on docs/vectors length mismatch")
	}
	if _, err := documentToRows(ctx, []*schema.Document{{ID: ""}}, [][]float64{{0.1}}); err == nil {
		t.Error("want error on empty doc id")
	}
}

func TestTruncateToRuneBoundary(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "short", in: "hello", max: 10, want: "hello"},
		{name: "exact", in: "hello", max: 5, want: "hello"},
		{name: "ascii cut", in: "hello", max: 4, want: "hell"},
		{name: "inside second rune", in: "中文中文", max: 4, want: "中"},
		{name: "rune edge", in: "中文中文", max: 3, want: "中"},
		{name: "inside first rune", in: "中文中文", max: 2, want: ""},
		{name: "mixed", in: "a中", max: 2, want: "a"},
		{name: "empty", in: "", max: 5, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncateToRuneBoundary(tc.in, tc.max); got != tc.want {
				t.Errorf("truncateToRuneBoundary(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

func TestDocumentToRowsTruncatesAtRuneBoundary(t *testing.T) {
	// Each 中 is 3 bytes; the padding makes the MaxContentLength cut land in
	// the middle of the first Chinese rune.
	padding := strings.Repeat("a", consts.MaxContentLength-1)
	content := padding + "中文"
	docs := []*schema.Document{{ID: "doc-rune", Content: content, MetaData: map[string]any{}}}
	vectors := [][]float64{{0.1}}

	rows, err := documentToRows(context.Background(), docs, vectors)
	if err != nil {
		t.Fatalf("documentToRows: %v", err)
	}
	row := rows[0].(*docRow)
	if len(row.Content) > consts.MaxContentLength {
		t.Errorf("content len = %d, exceeds %d", len(row.Content), consts.MaxContentLength)
	}
	if !utf8.ValidString(row.Content) {
		tail := row.Content
		if len(tail) > 16 {
			tail = tail[len(tail)-16:]
		}
		t.Errorf("truncated content is not valid UTF-8, tail bytes: %q", tail)
	}
	if row.Content != padding {
		t.Errorf("content len = %d, want the %d-byte ASCII prefix", len(row.Content), len(padding))
	}
}
