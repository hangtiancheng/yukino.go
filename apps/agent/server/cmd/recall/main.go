// Command recall tests the Milvus vector retriever by running a sample
// query against the knowledge base and printing the retrieved documents.
// It is useful for verifying that document indexing and retrieval work correctly.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/retriever"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	r, err := retriever.NewMilvusRetriever(ctx, cfg)
	if err != nil {
		log.Fatalf("create retriever: %v", err)
	}

	query := "What is the cause of service going offline?"
	fmt.Println("Q:", query)

	docs, err := r.Retrieve(ctx, query)
	if err != nil {
		log.Fatalf("retrieve: %v", err)
	}

	for _, doc := range docs {
		fmt.Println("A:", doc.Content)
	}
	fmt.Printf("Done, retrieved %d documents\n", len(docs))
}
