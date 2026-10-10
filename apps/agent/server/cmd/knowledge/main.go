package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/agent/knowledge_index_pipeline"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	dir := cfg.FileDir

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk dir: %w", err)
		}
		if d.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".md") {
			fmt.Printf("[skip] not a markdown file: %s\n", path)
			return nil
		}

		fmt.Printf("[start] indexing file: %s\n", path)
		return knowledge_index_pipeline.IndexFile(ctx, cfg, path)
	})
	if err != nil {
		log.Fatalf("index knowledge base: %v", err)
	}
	fmt.Println("[done] knowledge base indexing completed")
}
