package loader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMarkdownKeepsFencedHeadingsAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guide.md")
	content := "intro\n# First\nbody\n~~~markdown\n# code heading\n~~~\n# Second\n中文"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	docs, err := LoadMarkdown(context.Background(), path)
	if err != nil || len(docs) != 3 {
		t.Fatalf("docs=%v err=%v", docs, err)
	}
	if !strings.Contains(docs[1].Content, "# code heading") || docs[1].MetaData["title"] != "First" || docs[2].MetaData["title"] != "Second" {
		t.Fatalf("split: %+v %+v", docs[1], docs[2])
	}
	ids := map[string]bool{}
	for _, doc := range docs {
		if doc.ID == "" || ids[doc.ID] || doc.MetaData["_source"] != "guide.md" {
			t.Fatalf("document: %+v", doc)
		}
		ids[doc.ID] = true
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadMarkdown(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if docs := splitMarkdown("\n  ", "empty.md"); len(docs) != 0 {
		t.Fatalf("empty file: %v", docs)
	}
}
