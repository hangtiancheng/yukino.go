package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/retriever"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

type QueryInternalDocsInput struct {
	Query string `json:"query" jsonschema:"required" jsonschema_description:"Query string used to retrieve internal documents"`
}

type queryInternalDocResult struct {
	ID       string         `json:"id"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata"`
}

func NewQueryInternalDocsTool(cfg *config.Config) (tool.InvokableTool, error) {
	t, err := utils.InferOptionableTool(
		"query_internal_docs",
		"Search internal documentation and knowledge base for relevant information. Performs RAG to find similar documents and extract processing steps. Useful for understanding internal procedures, best practices, or step-by-step guides.",
		func(ctx context.Context, input *QueryInternalDocsInput, opts ...tool.Option) (string, error) {
			errPayload := func(err error) (string, error) {
				b, _ := json.Marshal(map[string]any{
					"success": false,
					"error":   err.Error(),
					"message": "Failed to search internal documentation",
				})
				return string(b), nil
			}

			rr, err := retriever.NewMilvusRetriever(ctx, cfg)
			if err != nil {
				return errPayload(fmt.Errorf("create retriever: %w", err))
			}
			resp, err := rr.Retrieve(ctx, input.Query)
			if err != nil {
				return errPayload(fmt.Errorf("retrieve docs: %w", err))
			}

			results := make([]queryInternalDocResult, 0, len(resp))
			for _, d := range resp {
				meta := d.MetaData
				if meta == nil {
					meta = map[string]any{}
				}
				results = append(results, queryInternalDocResult{
					ID:       d.ID,
					Content:  d.Content,
					Metadata: meta,
				})
			}

			b, err := json.Marshal(results)
			if err != nil {
				return errPayload(fmt.Errorf("marshal docs: %w", err))
			}
			return string(b), nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("infer query_internal_docs tool: %w", err)
	}
	return t, nil
}
