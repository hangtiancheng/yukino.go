package manager

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
)

// ============ 管家通信协议工具（第 8 章 §8.2.2）============
//
// 管家强制 tool_choice=required，LLM 只能通过 generate_response 与 server 通信。
// idea_king_api 是选题/热点检索专用工具（执行后结果当 observation 塞回，继续循环）。

// generateResponseTool 构建管家与 LLM 的通信协议工具（第 8 章 §8.2.2）。
//
// task_arguments 声明为 string（JSON 对象字符串）——第 8 章 §8.4.3 提到模型
// 常把 task_arguments 序列化成字符串，声明为 string 天然规避格式坑，服务端再解析。
//
// agents_pipeline 是顶层参数且带 Enum 硬约束（第 8 章 §8.4.4 Schema 枚举注入）：
// LLM 只能从合法产线里选，选不存在的键由模型层/Schema 兜底，服务端校验仍做最终防线。
// 枚举用函数现取——业务新增产线只改 pipeline_registry.go 与 core 的映射，此处自动跟随。
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
				// agents_pipeline 顶层硬约束：LLM 只能从合法产线枚举里选（§8.4.4）。
				// new_agents_plan 必须填；其它 task_type 忽略此字段。
				"agents_pipeline": {Type: "string", Description: "new_agents_plan 时必填：从服务端产线模板选一个（产线限定你可用的 chain agent 集合）",
					Enum: subtask.ValidPipelineNames()},
				"task_arguments": {Type: "string", Description: "随 task_type 变化的结构化参数（JSON 对象字符串）。new_agents_plan 固定结构：{agents}，agents 是链（你决定顺序/组成，agent_id 必须是所选产线的 chain agent），每项 {\"agent_id\":\"gold_scriptwriter\",\"input\":\"...\"}，input 不填时服务端用产线默认。push_card 结构：{card_type, label, layout, prompt_items:[{title,text,imageUrl}]}，如创作功能选择卡：{card_type:'创作功能选择卡', layout:'image-text', prompt_items:[{title:'AI成片'},{title:'AI笔记图'}]}。"},
			},
			Required: []string{"thought", "friendly_reply", "task_type"},
		},
	}
}

// ToolIdeaKingAPI 选题/热点检索专用工具。
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

// ManagerProtocolTools 返回管家可用的策略工具列表（约束①：只给协议工具）。
func ManagerProtocolTools() []FunctionDef {
	return []FunctionDef{generateResponseTool(), ToolIdeaKingAPI}
}

// ManagerToolNameAllowed 判断工具名是否在管家白名单内（约束②）。
func ManagerToolNameAllowed(name string) bool {
	for _, def := range ManagerProtocolTools() {
		if def.Name == name {
			return true
		}
	}
	return false
}

// RunIdeaKing 执行选题检索（mock 实现：返回示例选题，演示约束链通过后继续循环）。
// 保留旧签名兼容测试；新代码用 RunIdeaKingWithProvider。
func RunIdeaKing(ctx context.Context, arguments string) (string, bool, error) {
	return RunIdeaKingWithProvider(ctx, ideaking.NewMockProvider(), arguments)
}

// RunIdeaKingWithProvider 用指定 provider 执行选题检索。
// 返回 HotTopicResult 的 JSON（含 title_data:[{title,content,eid}]），
// 作为 observation 塞回；strategy 直出场景由 handleIdeaKing 直接消费，不走到这里。
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
