package manager

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
)

// ============ Manager communication protocol tools (chapter 8 §8.2.2) ============
//
// The manager forces tool_choice=required; the LLM can only communicate with the server via generate_response.
// idea_king_api is the dedicated topic/hotspot retrieval tool (its result is fed back as an observation and the loop continues).

// generateResponseTool builds the communication protocol tool between the manager and the LLM (chapter 8 §8.2.2).
//
// task_arguments is declared as string (a JSON object string) — chapter 8 §8.4.3 notes that models
// often serialize task_arguments as a string; declaring it as string naturally avoids the format pitfall, and the server parses it.
//
// agents_pipeline is a top-level parameter with a hard Enum constraint (chapter 8 §8.4.4 schema enum injection):
// the LLM can only pick from valid pipelines; picking a nonexistent key is caught by the model layer/schema, with server-side validation as the final defense.
// The enum is fetched on the fly — adding a pipeline only changes pipeline_registry.go and the core mapping; this follows automatically.
func generateResponseTool() FunctionDef {
	return FunctionDef{
		Name:        "generate_response",
		Description: "度加智能体消息协议：结构化返回响应内容和任务类型。这是你与 server 通信的唯一方式，禁止直接输出文本。task_arguments 是 JSON 对象字符串，随 task_type 变化。",
		Parameters: ParameterSpec{
			Type: "object",
			Properties: map[string]PropertyDef{
				"thought":        {Type: "string", Description: "你的思考过程（内部记录）"},
				"friendly_reply": {Type: "string", Description: "面向用户的友好回复"},
				"task_type": {Type: "string", Description: "任务类型枚举：only_reply（直接回答）/ push_card（推送可交互卡，如创作功能选择卡）/ new_agents_plan（派发子任务）/ execute_agent",
					Enum: []string{"only_reply", "push_card", "new_agents_plan", "execute_agent"}},
				// agents_pipeline top-level hard constraint: the LLM can only pick from the valid pipeline enum (§8.4.4).
				// Required for new_agents_plan; ignored by other task_types.
				"agents_pipeline": {Type: "string", Description: "new_agents_plan 时必填：从服务端产线模板选一个（产线限定你可用的 chain agent 集合）",
					Enum: subtask.ValidPipelineNames()},
				"task_arguments": {Type: "string", Description: "随 task_type 变化的结构化参数（JSON 对象字符串）。new_agents_plan 固定结构：{agents}，agents 是链（你决定顺序/组成，agent_id 必须是所选产线的 chain agent），每项 {\"agent_id\":\"gold_scriptwriter\",\"input\":\"...\"}，input 不填时服务端用产线默认。push_card 结构：{card_type, label, layout, prompt_items:[{title,text,imageUrl}]}，如创作功能选择卡：{card_type:'创作功能选择卡', layout:'image-text', prompt_items:[{title:'AI成片'},{title:'AI笔记图'}]}。"},
			},
			Required: []string{"thought", "friendly_reply", "task_type"},
		},
	}
}

// ToolIdeaKingAPI is the dedicated topic/hotspot retrieval tool.
var ToolIdeaKingAPI = FunctionDef{
	Name:        "idea_king_api",
	Description: "选题/热点检索专用工具。当用户需要找选题、热点、素材灵感时调用。",
	Parameters: ParameterSpec{
		Type: "object",
		Properties: map[string]PropertyDef{
			"query":         {Type: "string", Description: "检索关键词"},
			"hotspot_count": {Type: "number", Description: "热点条数，默认 5"},
		},
		Required: []string{"query"},
	},
}

// ManagerProtocolTools returns the strategy tool list available to the manager (constraint ①: protocol tools only).
func ManagerProtocolTools() []FunctionDef {
	return []FunctionDef{generateResponseTool(), ToolIdeaKingAPI}
}

// ManagerToolNameAllowed checks whether a tool name is in the manager whitelist (constraint ②).
func ManagerToolNameAllowed(name string) bool {
	for _, def := range ManagerProtocolTools() {
		if def.Name == name {
			return true
		}
	}
	return false
}

// RunIdeaKing performs topic retrieval (mock implementation: returns sample topics, demonstrating the loop continues after the constraint chain passes).
// Kept with the old signature for test compatibility; new code uses RunIdeaKingWithProvider.
func RunIdeaKing(ctx context.Context, arguments string) (string, bool, error) {
	return RunIdeaKingWithProvider(ctx, ideaking.NewMockProvider(), arguments)
}

// RunIdeaKingWithProvider performs topic retrieval with the given provider.
// It returns the HotTopicResult JSON (containing title_data:[{title,content,eid}]),
// fed back as an observation; the strategy direct-emission scenario is consumed by handleIdeaKing and never reaches here.
func RunIdeaKingWithProvider(ctx context.Context, p ideaking.IdeaKingProvider, arguments string) (string, bool, error) {
	params := ParseArgs(arguments)
	q := ArgString(params, "query", "")
	if q == "" {
		return "", false, fmt.Errorf("idea_king_api: query 必填")
	}
	count := ArgInt(params, "hotspot_count", 3)
	res, err := p.SearchHotTopics(ctx, q, count)
	if err != nil {
		return "", false, fmt.Errorf("idea_king_api: %w", err)
	}
	b, _ := json.Marshal(res)
	return string(b), false, nil
}
