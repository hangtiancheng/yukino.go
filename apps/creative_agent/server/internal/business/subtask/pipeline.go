package subtask

import "fmt"

// Pipeline 是 multi-agent 产线链路的 ID（如 "mix_video"）。
//
// 第 8 章 §8.4.4：LLM 决定链怎么装配，服务端只翻译。产线在本包定义「合法 agent 集合」
// （见下方 categoryToAgentMapping），供两处使用：manager（校验 LLM 填的每个 agent
// 属于所选产线）与 executor（点卡确认后 GetNextAgent 推进复用同一份映射），避免两套定义漂移。
type Pipeline string

// 预定义的 pipeline（对应真实系统 fasttask/agent_mapping.go 的 categoryToAgentMapping）。
const (
	PipelineMixVideo Pipeline = "mix_video" // 混剪视频产线
	PipelineArticle  Pipeline = "article"   // 图文产线
)

// AgentID 是智能体 ID。
type AgentID string

// 预定义的 agent（对应真实系统 fasttask 的 pipeline agents）。
const (
	// AgentIdeaKing 点子王——产线链首，产出选题（走 idea_king_api 工具，不进 plan）。
	AgentIdeaKing AgentID = "idea_king"
	// AgentGoldScriptwriter 金牌脚本师——产出脚本。
	AgentGoldScriptwriter AgentID = "gold_scriptwriter"
	// AgentEditMaster 剪辑大师——产出成片。
	AgentEditMaster AgentID = "edit_master"
)

// categoryToAgentMapping 是 pipeline → 合法 agent 集合映射（05-subagent.md §5.5）。
// 决定 GetNextAgent 的"下一个是谁"，也决定管家校验 LLM 填的 agent 是否属于所选产线。
var categoryToAgentMapping = map[Pipeline][]AgentID{
	PipelineMixVideo: {AgentIdeaKing, AgentGoldScriptwriter, AgentEditMaster},
	PipelineArticle:  {AgentIdeaKing, AgentGoldScriptwriter},
}

// GetPipelineAgents 返回 pipeline 的合法 agent 集合（含选题 agent idea_king）。
//
// 第 8 章 §8.4.4：LLM 填的每个 agent_id 必须在此集合内（防越权），链的顺序/组成由
// LLM 决定。这里作为唯一数据源，供管家校验与 fastpath GetNextAgent 共用。
func GetPipelineAgents(pipeline Pipeline) ([]AgentID, error) {
	agents, ok := categoryToAgentMapping[pipeline]
	if !ok {
		return nil, fmt.Errorf("unknown pipeline: %s", pipeline)
	}
	out := make([]AgentID, len(agents))
	copy(out, agents)
	return out, nil
}

// GetNextAgent 计算 pipeline 中当前 agent 的下一个。
//
// 对应真实系统 fasttask/agent_mapping.go:46 的 GetNextAgent。
// 当前 agent 不在 pipeline 中 → 返回第一个；已是最后一个 → 返回空（链结束）。
func GetNextAgent(pipeline Pipeline, current AgentID) (AgentID, error) {
	agents, ok := categoryToAgentMapping[pipeline]
	if !ok {
		return "", fmt.Errorf("unknown pipeline: %s", pipeline)
	}
	for i, a := range agents {
		if a == current {
			if i+1 < len(agents) {
				return agents[i+1], nil
			}
			return "", nil // 已是最后一个，链结束
		}
	}
	// 当前 agent 不在映射里（可能是新会话首次）→ 从第一个开始
	if len(agents) > 0 {
		return agents[0], nil
	}
	return "", nil
}
