package subtask

import "fmt"

// Pipeline is the ID of a multi-agent pipeline chain (e.g. "mix_video").
//
// Chapter 8 §8.4.4: the LLM decides how to assemble the chain; the server only translates. Pipelines define the "valid agent set" in this package
// (see categoryToAgentMapping below), used in two places: the manager (validating that every LLM-filled agent
// belongs to the selected pipeline) and the executor (GetNextAgent advancement after card confirmation reuses the same mapping), avoiding two drifting definitions.
type Pipeline string

// Predefined pipelines (corresponding to the reference system's fasttask/agent_mapping.go categoryToAgentMapping).
const (
	PipelineMixVideo Pipeline = "mix_video" // mix-cut video pipeline
	PipelineArticle  Pipeline = "article"   // image-text article pipeline
)

// AgentID is the agent ID.
type AgentID string

// Predefined agents (corresponding to the reference system's fasttask pipeline agents).
const (
	// AgentIdeaKing is the idea king — head of the pipeline chain, producing topics (via the idea_king_api tool; does not enter the plan).
	AgentIdeaKing AgentID = "idea_king"
	// AgentGoldScriptwriter is the gold scriptwriter — producing scripts.
	AgentGoldScriptwriter AgentID = "gold_scriptwriter"
	// AgentEditMaster is the edit master — producing the final video.
	AgentEditMaster AgentID = "edit_master"
)

// categoryToAgentMapping is the pipeline → valid agent set mapping (05-subagent.md §5.5).
// It decides GetNextAgent's "who is next" and also the manager's validation that LLM-filled agents belong to the selected pipeline.
var categoryToAgentMapping = map[Pipeline][]AgentID{
	PipelineMixVideo: {AgentIdeaKing, AgentGoldScriptwriter, AgentEditMaster},
	PipelineArticle:  {AgentIdeaKing, AgentGoldScriptwriter},
}

// GetPipelineAgents returns the valid agent set of a pipeline (including the topic agent idea_king).
//
// Chapter 8 §8.4.4: every LLM-filled agent_id must be in this set (preventing overreach); the chain's order/composition is
// decided by the LLM. This is the single source of truth shared by manager validation and fastpath GetNextAgent.
func GetPipelineAgents(pipeline Pipeline) ([]AgentID, error) {
	agents, ok := categoryToAgentMapping[pipeline]
	if !ok {
		return nil, fmt.Errorf("unknown pipeline: %s", pipeline)
	}
	out := make([]AgentID, len(agents))
	copy(out, agents)
	return out, nil
}

// GetNextAgent computes the next agent after the current one in the pipeline.
//
// Corresponds to the reference system's fasttask/agent_mapping.go:46 GetNextAgent.
// The current agent not in the pipeline → returns the first; already the last → returns empty (chain end).
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
			return "", nil // already the last; chain end
		}
	}
	// The current agent is not in the mapping (possibly the first run of a new session) → start from the first
	if len(agents) > 0 {
		return agents[0], nil
	}
	return "", nil
}
