package main

import (
	"context"
	"fmt"
	"log"

	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/ai/agent/plan_execute_replan"
	"github.com/hangtiancheng/yukino.go/apps/agent/server/internal/config"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	resp, detail, err := plan_execute_replan.BuildPlanAgent(ctx, cfg, plan_execute_replan.AIOpsQuery)
	if err != nil {
		log.Fatalf("build plan agent: %v", err)
	}

	fmt.Println("----- Final Response -----")
	fmt.Println(resp)
	fmt.Println("----- Execution Detail -----")
	for i, d := range detail {
		fmt.Printf("[%d] %s\n", i, d)
	}
}
