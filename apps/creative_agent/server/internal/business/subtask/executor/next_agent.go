package subtaskexecutor

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
)

// Pipeline / AgentID 及 pipeline → agent 集合映射定义在 subtask 根包 pipeline.go。
// subtask 根包持有 agent 集合唯一数据源，供 manager（管家校验 LLM 填的 agent 属于产线）
// 与 fastpath（点卡确认后 GetNextAgent 推进）共用，避免业务包之间的依赖环。

// BuildExecuteAgent 从当前 agent 构造下一个 task_run 的 plan_steps（单 agent）。
//
// 对应真实系统 buildExecuteAgent（judge.go:172）：每次投递单个 execute_agent，
// planSteps 里只有当前这一个 agent（执行器只吃 Agents[0]）。
//
// scriptContent 是前序脚本（可选）：点卡确认后由 fastpath 提取，拼进下一个
// agent 的 input（对齐真实系统 BuildExecuteAgentInvokeMessage——edit_master
// 需要拿到脚本才能调产线工具）。为空时退化为只用 query。
func BuildExecuteAgent(pipeline subtask.Pipeline, current subtask.AgentID, userID int64, sessionID, agentID, query, scriptContent string) ([]model.PlanAgent, error) {
	next, err := subtask.GetNextAgent(pipeline, current)
	if err != nil {
		return nil, err
	}
	if next == "" {
		return nil, nil // 链结束
	}
	input := query
	if scriptContent != "" {
		// 脚本内容置前，用户确认语置后（对齐真实系统 invoke_message 结构：
		// "【任务指令】... 前序脚本内容：{script} 创作要求：..."）。
		input = scriptContent + "\n\n" + query
	}
	return []model.PlanAgent{
		{AgentID: string(next), Input: input},
	}, nil
}
