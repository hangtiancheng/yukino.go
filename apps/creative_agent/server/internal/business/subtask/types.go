package subtask

import (
	appkitrunner "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/runner"
)

// ============ Type aliases: subtask → provider (component layer) ============
//
// The subtask business package reuses the type definitions of the provider/runner component layer; aliases keep references concise.
// Dependency direction subtask → runner (one-way, no cycle).

// PlanAgentPayload is one agent in the plan (used when translating pipelines → plan_steps).
type PlanAgentPayload = appkitrunner.PlanAgentPayload
