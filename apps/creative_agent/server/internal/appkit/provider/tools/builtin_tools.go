package appkittools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/baseapi"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/fakegen"
)

// ============ Built-in business tools (sub-agent pipeline tools + manager scheduling tools) ============
//
// In the replica scenario these tools are "fake implementations" — they do not call real external pipelines but return simulated products.
// However, the schemas and execution flow match the reference system: full FunctionDef + ToolRunner,
// and pipeline tools with isPipeline=true trigger NeedWait branching (see 07-tool-system.md §7.9).
//
// Tool list:
//   - search            knowledge-base search (shared by manager/sub-agents, sync)
//   - generate_image    image generation (sub-agent execution class, sync)
//   - create_video_task submit a video synthesis task (sub-agent pipeline class, isPipeline=true)
//   - dispatch_agent    dispatch subtasks (manager scheduling class)

// Agent constants (mirroring internal/business/subtask/pipeline.go, avoiding a dependency cycle from the tools package to business packages).
const (
	// AgentGoldScriptwriter is the gold scriptwriter — writes scripts (no pipeline tools).
	AgentGoldScriptwriter = "gold_scriptwriter"
	// AgentEditMaster is the edit master — edits videos (the only agent that can call the pipeline tool create_video_task).
	AgentEditMaster = "edit_master"
)

// ToolSearch searches the knowledge base for relevant information.
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

// ToolGenerateImage generates an image (returns the image URL synchronously).
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

// ToolCreateVideoTask submits a video synthesis task to the pipeline (async pipeline tool, isPipeline=true).
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

// ToolDispatchAgent dispatches subtasks (manager scheduling tool).
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

// RunSearch executes the search tool.
func RunSearch(ctx context.Context, arguments string) (string, bool, error) {
	params := ParseArgs(arguments)
	q := ArgString(params, "query", "")
	if q == "" {
		return "", false, fmt.Errorf("search: query 必填")
	}
	return fmt.Sprintf(`{"results":[{"title":"关于「%s」的资料","url":"https://example.com/%s"}]}`, q, q), false, nil
}

// RunGenerateImage executes the image-generation tool (fake implementation, returns a simulated URL).
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

// RunCreateVideoTask executes the video-synthesis tool (fake implementation; isPipeline=true triggers NeedWait).
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

// RunCreateVideoTaskReal executes the real four-call chain of create_video_task:
// pre_create → create → fakegen.submit → async_post_create.
// It returns the pipeline task_id + isPipeline=true (triggering Waiting).
func RunCreateVideoTaskReal(deps RealRunnerDeps) ToolRunner {
	return func(ctx context.Context, arguments string) (string, bool, error) {
		params := ParseArgs(arguments)
		prompt := ArgString(params, "prompt", "")
		if prompt == "" {
			return "", false, fmt.Errorf("create_video_task: prompt 必填")
		}
		// Identity propagation: read only from ctx (the run.UserID/SessionID injected by the executor).
		// Tool arguments are not read — identity must come from a trusted source; LLM arguments cannot inject it.
		userID, sessionID := IdentityFromContext(ctx)

		// ① pre_create: assemble the downstream operator inputs
		preReq := &baseapi.PreCreateReq{
			ProjectType:  "mix_video",
			RoleType:     5, // edit master
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
			// Failure card / risk-control block: do not enter generation
			out := struct {
				Status   int    `json:"status"`
				TaskType string `json:"task_type"`
				CardType string `json:"card_type,omitempty"`
			}{preResp.Data.Status, preResp.Data.TaskType, cardTypeFromArgs(preResp.Data.TaskArguments)}
			b, _ := json.Marshal(out)
			return string(b), false, nil
		}

		// ② create: deduct points + create the work
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

		// ③ fakegen.submit: start the simulated generation and obtain the pipeline task_id
		// The pipeline ID is assigned atomically by fakegen (passing TaskID 0 triggers nextTaskID.Add(1)),
		// avoiding go-learn's timestamp-modulo collisions under parallel agents; the returned task_id is passed through to async_post_create.
		submitReq := &fakegen.SubmitReq{
			ProjectInput: preResp.Data.ProjectInput,
			// The callback entry point is go-learn itself (Waiting→Finished is advanced through it), not base-server.
			CallbackURL: deps.CallbackBaseURL + "/aigc/callback/task_detail",
			TaskID:      0,
			SessionID:   sessionID,
			UserID:      userID,
		}
		taskID, err := deps.FakeGen.Submit(ctx, submitReq)
		if err != nil {
			return "", false, fmt.Errorf("fakegen submit: %w", err)
		}

		// ④ async_post_create: register the pipeline ID
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

// cardTypeFromArgs extracts the card_type string from task_arguments (distinguishing input-risk cards / insufficient-points cards, etc.).
func cardTypeFromArgs(args map[string]any) string {
	v, _ := args["card_type"].(string)
	return v
}

// RunDispatchAgent executes the subtask-dispatch tool (fake implementation, returns a dispatch confirmation).
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

// RealRunnerDeps holds the dependencies of the real pipeline Runner (base-server client + fakegen server).
type RealRunnerDeps struct {
	Base            *baseapi.Client
	FakeGen         *fakegen.Server
	CallbackBaseURL string // go-learn's own address (the callback entry point), e.g. http://localhost:8080
}

// NewSubAgentToolRegistry builds the sub-agent tool registry (execution class + pipeline class).
// When deps is nil, create_video_task falls back to the fake implementation (the demo does not depend on external services).
//
// Per-agent tool-set isolation (aligned with the reference system's GetFunctions returning the agent's tool list by agentID):
//   - Global defaults (visible to all agents): search + generate_image
//   - edit_master exclusive: + create_video_task (only the edit master can call the pipeline tool)
//
// This way gold_scriptwriter (script writing) cannot see the pipeline tool and cannot "call the pipeline directly after writing the script, skipping the script card";
// it can only output the script JSON → push the script card → user confirms → edit_master → call the pipeline. The chain's determinism is guaranteed by the tool set.
func NewSubAgentToolRegistry(deps ...*RealRunnerDeps) *ToolRegistry {
	r := NewToolRegistry()
	r.Register(ToolEntry{Def: ToolSearch, Runner: RunSearch})
	r.Register(ToolEntry{Def: ToolGenerateImage, Runner: RunGenerateImage})
	r.Register(ToolEntry{Def: ToolCreateVideoTask, Runner: RunCreateVideoTask})
	if len(deps) > 0 && deps[0] != nil {
		r.tools[ToolCreateVideoTask.Name].Runner = RunCreateVideoTaskReal(*deps[0])
	}

	// Global defaults: search + generate_image (no agentID context / agents without exclusive tools).
	for _, def := range []FunctionDef{ToolSearch, ToolGenerateImage} {
		r.RegisterDefault(def)
	}
	// edit_master exclusive: the pipeline tool is visible only to the edit master.
	for _, def := range []FunctionDef{ToolSearch, ToolGenerateImage, ToolCreateVideoTask} {
		r.RegisterDefaultForAgent(AgentEditMaster, def)
	}
	return r
}

// hashStr computes a simple hash of a string (for simulated URLs/IDs, not cryptographic).
func hashStr(s string) uint64 {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}
