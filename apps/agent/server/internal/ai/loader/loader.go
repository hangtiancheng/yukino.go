package loader

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/document"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/consts"
)

func LoadMarkdown(ctx context.Context, path string) ([]*document.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return splitMarkdown(string(raw), filepath.Base(path)), nil
}
func splitMarkdown(content, source string) []*document.Document {
	var docs []*document.Document
	var chunk []string
	title := ""
	fence := byte(0)
	fenceLen := 0
	flush := func() {
		text := strings.TrimSpace(strings.Join(chunk, "\n"))
		if text != "" {
			docs = append(docs, &document.Document{ID: uuid.NewString(), Content: text, MetaData: map[string]any{consts.MilvusSourceKey: source, "title": title}})
		}
		chunk = nil
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if indent <= 3 && len(trimmed) > 0 && (trimmed[0] == '~' || trimmed[0] == 96) {
			marker := trimmed[0]
			count := 0
			for count < len(trimmed) && trimmed[count] == marker {
				count++
			}
			if count >= 3 {
				if fence == 0 {
					fence = marker
					fenceLen = count
				} else if fence == marker && count >= fenceLen && strings.TrimSpace(trimmed[count:]) == "" {
					fence = 0
				}
			}
		}
		if fence == 0 && indent <= 3 && strings.HasPrefix(trimmed, "# ") {
			flush()
			title = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(trimmed[2:]), " #"))
		}
		chunk = append(chunk, line)
	}
	flush()
	return docs
}
