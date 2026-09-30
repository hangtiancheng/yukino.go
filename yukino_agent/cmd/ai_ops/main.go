// Command ai_ops runs the plan-execute-replan agent standalone to perform
// intelligent alert operations analysis. It executes a predefined prompt that
// queries Prometheus alerts, retrieves internal documentation, and generates
// a structured alert operations report.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/ai/agent/plan_execute_replan"
	"github.com/hangtiancheng/yukino.go/yukino_agent/internal/config"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	resp, detail, err := plan_execute_replan.BuildPlanAgent(ctx, cfg, plan_execute_replan.AIOnOpsQuery)
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
