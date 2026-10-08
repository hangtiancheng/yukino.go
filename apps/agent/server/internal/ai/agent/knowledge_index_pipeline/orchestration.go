// Package knowledge_index_pipeline implements the document indexing pipeline.
// It loads documents from the filesystem, splits them into chunks using Markdown
// header-based splitting, embeds the chunks, and stores them in Milvus.
//
// Pipeline flow:
//
//	Source -> [FileLoader] -> [MarkdownSplitter] -> [MilvusIndexer] -> IDs
package knowledge_index_pipeline

import (
	"context"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/compose"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

// BuildKnowledgeIndexing constructs and compiles the knowledge indexing pipeline graph.
// The pipeline loads a document, splits it by Markdown headers, and indexes the
// resulting chunks into Milvus.
func BuildKnowledgeIndexing(ctx context.Context, cfg *config.Config) (compose.Runnable[document.Source, []string], error) {
	const (
		FileLoader       = "FileLoader"
		MarkdownSplitter = "MarkdownSplitter"
		MilvusIndexer    = "MilvusIndexer"
	)

	g := compose.NewGraph[document.Source, []string]()

	fileLoader, err := newLoader(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddLoaderNode(FileLoader, fileLoader)

	markdownSplitter, err := newDocumentTransformer(ctx)
	if err != nil {
		return nil, err
	}
	_ = g.AddDocumentTransformerNode(MarkdownSplitter, markdownSplitter)

	milvusIndexer, err := newIndexer(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_ = g.AddIndexerNode(MilvusIndexer, milvusIndexer)

	_ = g.AddEdge(compose.START, FileLoader)
	_ = g.AddEdge(MilvusIndexer, compose.END)
	_ = g.AddEdge(FileLoader, MarkdownSplitter)
	_ = g.AddEdge(MarkdownSplitter, MilvusIndexer)

	return g.Compile(ctx, compose.WithGraphName("KnowledgeIndexing"), compose.WithNodeTriggerMode(compose.AnyPredecessor))
}
