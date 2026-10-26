package appkittools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/baseapi"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/fakegen"
)

// ============ 内建业务工具（子 Agent 产线工具 + 管家调度工具）============
//
// 复刻场景下这些工具是"假实现"——不真正调用外部产线，而是返回模拟产物。
// 但 Schema 与执行流程与真实系统一致：完整的 FunctionDef + ToolRunner，
// isPipeline=true 的产线工具会触发 NeedWait 分流（见 07-tool-system.md §7.9）。
//
// 工具清单：
//   - search            搜索知识库（管家/子 Agent 通用，同步）
//   - generate_image    生成图片（子 Agent 执行类，同步）
//   - create_video_task 提交视频合成任务（子 Agent 产线类，isPipeline=true）
//   - dispatch_agent    派发子任务（管家调度类）

// Agent 常量（镜像 internal/business/subtask/pipeline.go，避免 tools 包依赖业务包造成环）。
const (
	// AgentGoldScriptwriter 金牌脚本师——写脚本（无产线工具）。
	AgentGoldScriptwriter = "gold_scriptwriter"
	// AgentEditMaster 剪辑大师——剪视频（唯一可调产线工具 create_video_task 的 agent）。
	AgentEditMaster = "edit_master"
)

// ToolSearch 搜索知识库获取相关信息。
var ToolSearch = FunctionDef{
	Name:        "search",
	Description: "搜索知识库获取相关信息。当用户提问需要外部知识时调用。",
	Parameters: ParameterSpec{
		Type: "object",
		Properties: map[string]PropertyDef{
			"query": {Type: "string", Description: "搜索关键词"},
		},
		Required: []string{"query"},
	},
}

// ToolGenerateImage 生成图片（同步返回图片 URL）。
var ToolGenerateImage = FunctionDef{
	Name:        "generate_image",
	Description: "根据提示词生成图片，同步返回图片URL。用于海报/插画等视觉素材产出。",
	Parameters: ParameterSpec{
		Type: "object",
		Properties: map[string]PropertyDef{
			"prompt":      {Type: "string", Description: "图片描述"},
			"image_ratio": {Type: "string", Description: "图片比例", Enum: []string{"1:1", "16:9", "9:16"}},
		},
		Required: []string{"prompt"},
	},
}

// ToolCreateVideoTask 提交视频合成任务到产线（异步产线工具，isPipeline=true）。
var ToolCreateVideoTask = FunctionDef{
	Name:        "create_video_task",
	Description: "提交视频合成任务到产线。产线完成后会回调通知。视频合成是异步的，提交后等待产线回调。",
	Parameters: ParameterSpec{
		Type: "object",
		Properties: map[string]PropertyDef{
			"prompt":   {Type: "string", Description: "视频内容描述"},
			"duration": {Type: "number", Description: "视频时长（秒）"},
		},
		Required: []string{"prompt"},
	},
}

// ToolDispatchAgent 派发子任务（管家调度工具）。
var ToolDispatchAgent = FunctionDef{
	Name:        "dispatch_agent",
	Description: "派发子任务给指定智能体执行。当用户请求需要多步骤的产线协作时调用。",
	Parameters: ParameterSpec{
		Type: "object",
		Properties: map[string]PropertyDef{
			"agent_id": {Type: "string", Description: "目标智能体 ID（如 gold_scriptwriter）"},
			"input":    {Type: "string", Description: "传给该智能体的任务输入"},
		},
		Required: []string{"agent_id", "input"},
	},
}

// RunSearch 执行搜索工具。
func RunSearch(ctx context.Context, arguments string) (string, bool, error) {
	params := ParseArgs(arguments)
	q := ArgString(params, "query", "")
	if q == "" {
		return "", false, fmt.Errorf("search: query 必填")
	}
	return fmt.Sprintf(`{"results":[{"title":"关于「%s」的资料","url":"https://example.com/%s"}]}`, q, q), false, nil
}

// RunGenerateImage 执行图片生成工具（假实现，返回模拟 URL）。
func RunGenerateImage(ctx context.Context, arguments string) (string, bool, error) {
	params := ParseArgs(arguments)
	prompt := ArgString(params, "prompt", "")
	if prompt == "" {
		return "", false, fmt.Errorf("generate_image: prompt 必填")
	}
	ratio := ArgString(params, "image_ratio", "1:1")
	img := struct {
		Prompt string `json:"prompt"`
		Ratio  string `json:"image_ratio"`
		URL    string `json:"url"`
	}{
		Prompt: prompt,
		Ratio:  ratio,
		URL:    fmt.Sprintf("https://img.example.com/gen/%d.png", hashStr(prompt)),
	}
	b, _ := json.Marshal(img)
	return string(b), false, nil
}

// RunCreateVideoTask 执行视频合成工具（假实现，isPipeline=true 触发 NeedWait）。
func RunCreateVideoTask(ctx context.Context, arguments string) (string, bool, error) {
	params := ParseArgs(arguments)
	prompt := ArgString(params, "prompt", "")
	if prompt == "" {
		return "", false, fmt.Errorf("create_video_task: prompt 必填")
	}
	taskID := fmt.Sprintf("pipeline-%d", hashStr(prompt))
	out := struct {
		PipelineTaskID string `json:"pipeline_task_id"`
		Status         string `json:"status"`
	}{
		PipelineTaskID: taskID,
		Status:         "submitted",
	}
	b, _ := json.Marshal(out)
	return string(b), true, nil // isPipeline=true
}

// RunCreateVideoTaskReal 执行 create_video_task 的真实四连：
// pre_create → create → fakegen.submit → async_post_create。
// 返回产线 task_id + isPipeline=true（触发 Waiting）。
func RunCreateVideoTaskReal(deps RealRunnerDeps) ToolRunner {
	return func(ctx context.Context, arguments string) (string, bool, error) {
		params := ParseArgs(arguments)
		prompt := ArgString(params, "prompt", "")
		if prompt == "" {
			return "", false, fmt.Errorf("create_video_task: prompt 必填")
		}
		// 身份透传：只从 ctx 读（executor 注入的 run.UserID/SessionID）。
		// 不读工具参数——身份必须来自可信来源，LLM 参数不可注入。
		userID, sessionID := IdentityFromContext(ctx)

		// ① pre_create：组装下游算子入参
		preReq := &baseapi.PreCreateReq{
			ProjectType:  "mix_video",
			RoleType:     5, // 剪辑大师
			ProjectInput: fmt.Sprintf(`{"input":%q}`, prompt),
			UserID:       userID,
			SessionID:    sessionID,
		}
		preResp, err := deps.Base.PreCreate(ctx, preReq)
		if err != nil {
			return "", false, fmt.Errorf("pre_create: %w", err)
		}
		if preResp.Data == nil {
			return "", false, fmt.Errorf("pre_create: empty data")
		}
		if preResp.Data.Status != 0 {
			// 失败卡 / 风控拦截：不进入生成
			out := struct {
				Status   int    `json:"status"`
				TaskType string `json:"task_type"`
				CardType string `json:"card_type,omitempty"`
			}{preResp.Data.Status, preResp.Data.TaskType, cardTypeFromArgs(preResp.Data.TaskArguments)}
			b, _ := json.Marshal(out)
			return string(b), false, nil
		}

		// ② create：扣积分 + 建作品
		createReq := &baseapi.CreateReq{
			ProjectType:  "mix_video",
			ProjectInput: map[string]any{"input": prompt},
			Title:        "创作任务",
			UserID:       userID,
			SessionID:    sessionID,
		}
		createResp, err := deps.Base.Create(ctx, createReq)
		if err != nil {
			return "", false, fmt.Errorf("create: %w", err)
		}
		if createResp.Data != nil && createResp.Data.Status == "insufficient_score" {
			return `{"status":"insufficient_score"}`, false, nil
		}

		// ③ fakegen.submit：模拟生成启动，拿产线 task_id
		// 产线 ID 由 fakegen 原子分配（TaskID 传 0 触发 nextTaskID.Add(1)），
		// 避免 go-learn 时间戳取模在并行 agent 下碰撞；返回的 task_id 透传给 async_post_create。
		submitReq := &fakegen.SubmitReq{
			ProjectInput: preResp.Data.ProjectInput,
			// 回调入口在 go-learn 自身（Waiting→Finished 靠它推进），不是 base-server。
			CallbackURL: deps.CallbackBaseURL + "/aigc/callback/task_detail",
			TaskID:      0,
			SessionID:   sessionID,
			UserID:      userID,
		}
		taskID, err := deps.FakeGen.Submit(ctx, submitReq)
		if err != nil {
			return "", false, fmt.Errorf("fakegen submit: %w", err)
		}

		// ④ async_post_create：登记产线 ID
		asyncReq := &baseapi.AsyncPostCreateReq{
			SessionID:   sessionID,
			UserID:      userID,
			TaskOutput:  fmt.Sprintf(`{"errno":0,"data":{"task_id":%d,"task_status":"success"}}`, taskID),
			Status:      "success",
			RoleType:    5,
			ProjectType: "mix_video",
		}
		if _, err := deps.Base.AsyncPostCreate(ctx, asyncReq); err != nil {
			return "", false, fmt.Errorf("async_post_create: %w", err)
		}

		out := struct {
			PipelineTaskID int64  `json:"pipeline_task_id"`
			Status         string `json:"status"`
		}{PipelineTaskID: taskID, Status: "submitted"}
		b, _ := json.Marshal(out)
		return string(b), true, nil
	}
}

// cardTypeFromArgs 从 task_arguments 里取 card_type 字符串（区分输入风控卡/积分不足卡等）。
func cardTypeFromArgs(args map[string]any) string {
	v, _ := args["card_type"].(string)
	return v
}

// RunDispatchAgent 执行派发子任务工具（假实现，返回派发确认）。
func RunDispatchAgent(ctx context.Context, arguments string) (string, bool, error) {
	params := ParseArgs(arguments)
	agentID := ArgString(params, "agent_id", "")
	input := ArgString(params, "input", "")
	if agentID == "" {
		return "", false, fmt.Errorf("dispatch_agent: agent_id 必填")
	}
	out := struct {
		AgentID string `json:"agent_id"`
		Input   string `json:"input"`
		Status  string `json:"status"`
	}{
		AgentID: agentID,
		Input:   input,
		Status:  "dispatched",
	}
	b, _ := json.Marshal(out)
	return string(b), false, nil
}

// RealRunnerDeps 持有真实产线 Runner 的依赖（base-server client + fakegen server）。
type RealRunnerDeps struct {
	Base            *baseapi.Client
	FakeGen         *fakegen.Server
	CallbackBaseURL string // go-learn 自身地址（回调入口），如 http://localhost:8080
}

// NewSubAgentToolRegistry 构建子 Agent 的工具注册表（执行类 + 产线类）。
// deps 为 nil 时 create_video_task 回退假实现（demo 不依赖外部服务）。
//
// 按 agent 隔离工具集（对齐真实系统 GetFunctions 按 agentID 返回该 agent 工具列表）：
//   - 全局默认（所有 agent 可见）：search + generate_image
//   - edit_master 专属：+ create_video_task（产线工具只有剪辑大师能调）
//
// 这样 gold_scriptwriter（写脚本）看不到产线工具，无法"写完脚本直接调产线跳过脚本卡"，
// 只能输出脚本 JSON → 推脚本卡 → 用户确认 → edit_master → 调产线。链路确定性由工具集保证。
func NewSubAgentToolRegistry(deps ...*RealRunnerDeps) *ToolRegistry {
	r := NewToolRegistry()
	r.Register(ToolEntry{Def: ToolSearch, Runner: RunSearch})
	r.Register(ToolEntry{Def: ToolGenerateImage, Runner: RunGenerateImage})
	r.Register(ToolEntry{Def: ToolCreateVideoTask, Runner: RunCreateVideoTask})
	if len(deps) > 0 && deps[0] != nil {
		r.tools[ToolCreateVideoTask.Name].Runner = RunCreateVideoTaskReal(*deps[0])
	}

	// 全局默认：search + generate_image（无 agentID 上下文 / 未配置专属工具的 agent）。
	for _, def := range []FunctionDef{ToolSearch, ToolGenerateImage} {
		r.RegisterDefault(def)
	}
	// edit_master 专属：产线工具只对剪辑大师可见。
	for _, def := range []FunctionDef{ToolSearch, ToolGenerateImage, ToolCreateVideoTask} {
		r.RegisterDefaultForAgent(AgentEditMaster, def)
	}
	return r
}

// hashStr 生成字符串的简单哈希（用于模拟 URL/ID，非加密用途）。
func hashStr(s string) uint64 {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}
