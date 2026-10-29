package subtaskexecutor

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
)

// Pipeline / AgentID and the pipeline → agent-set mapping are defined in the subtask root package's pipeline.go.
// The subtask root package holds the single source of truth for agent sets, shared by the manager (validating LLM-filled agents belong to the pipeline)
// and fastpath (GetNextAgent advancement after card confirmation), avoiding dependency cycles between business packages.

// BuildExecuteAgent builds the next task_run's plan_steps (single agent) from the current agent.
//
// Corresponds to the reference system's buildExecuteAgent (judge.go:172): each dispatch is a single execute_agent;
// planSteps contains only this one agent (the executor consumes only Agents[0]).
//
// scriptContent is the preceding script (optional): extracted by fastpath after card confirmation and spliced into the next
// agent's input (aligned with the reference system's BuildExecuteAgentInvokeMessage — edit_master
// needs the script before it can call the pipeline tool). When empty it degrades to using only the query.
func BuildExecuteAgent(pipeline subtask.Pipeline, current subtask.AgentID, userID int64, sessionID, agentID, query, scriptContent string) ([]model.PlanAgent, error) {
	next, err := subtask.GetNextAgent(pipeline, current)
	if err != nil {
		return nil, err
	}
	if next == "" {
		return nil, nil // chain end
	}
	input := query
	if scriptContent != "" {
		// Script content first, user confirmation text after (aligned with the reference system's invoke_message structure:
		// "【任务指令】... 前序脚本内容：{script} 创作要求：...").
		input = scriptContent + "\n\n" + query
	}
	return []model.PlanAgent{
		{AgentID: string(next), Input: input},
	}, nil
}
