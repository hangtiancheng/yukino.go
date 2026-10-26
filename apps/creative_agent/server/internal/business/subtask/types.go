package subtask

import (
	appkitrunner "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/runner"
)

// ============ 类型别名：subtask → provider（组件层）============
//
// subtask 业务包复用 provider/runner 组件层的类型定义，通过别名保持本包内引用简洁。
// 依赖方向 subtask → runner（单向，无环）。

// PlanAgentPayload 是计划中的一个 agent（translate 产线 → plan_steps 用）。
type PlanAgentPayload = appkitrunner.PlanAgentPayload
