package appkittools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/baseapi"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/fakegen"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestToolRegistry_RegisterAndGet verifies ToolRegistry returns registered defaults.
func TestToolRegistry_RegisterAndGet(t *testing.T) {
	r := appkittools.NewToolRegistry()
	r.Register(appkittools.ToolEntry{Def: appkittools.ToolSearch, Runner: appkittools.RunSearch})
	r.RegisterDefault(appkittools.ToolSearch)

	defs, err := r.GetFunctionDefs(context.Background())
	testx.NoError(t, err)
	testx.Len(t, defs, 1)
	testx.Equal(t, "search", defs[0].Name)
	testx.NotEmpty(t, defs[0].Description, "description 决定 LLM 何时选用工具，必填")
	testx.Equal(t, "object", defs[0].Parameters.Type)
	testx.Contains(t, defs[0].Parameters.Required, "query")
}

// TestToolRegistry_Execute verifies Execute dispatches to the right runner.
func TestToolRegistry_Execute(t *testing.T) {
	r := appkittools.NewSubAgentToolRegistry()

	// 命中：同步工具
	out, handled, isPipeline, err := r.Execute(context.Background(), "generate_image", `{"prompt":"一只猫"}`)
	testx.NoError(t, err)
	testx.True(t, handled)
	testx.False(t, isPipeline)
	var img struct {
		URL string `json:"url"`
	}
	testx.NoError(t, json.Unmarshal([]byte(out), &img))
	testx.Contains(t, img.URL, "https://img.example.com/gen/")

	// 产线工具：isPipeline=true（触发 NeedWait 分流）
	out2, handled2, isPipeline2, err := r.Execute(context.Background(), "create_video_task", `{"prompt":"风景"}`)
	testx.NoError(t, err)
	testx.True(t, handled2)
	testx.True(t, isPipeline2, "create_video_task 是产线工具")
	testx.Contains(t, out2, "submitted")

	// 未注册：handled=false
	_, handled3, _, err := r.Execute(context.Background(), "no_such_tool", `{}`)
	testx.False(t, handled3)
	testx.Error(t, err)
}

// TestToolRegistry_ExecuteErrorAsObservation verifies tool errors do not interrupt
// the loop — they surface as error strings (observation).
func TestToolRegistry_ExecuteErrorAsObservation(t *testing.T) {
	r := appkittools.NewSubAgentToolRegistry()

	// 缺必填参数 → runner 报错
	_, handled, _, err := r.Execute(context.Background(), "generate_image", `{}`)
	testx.True(t, handled)
	testx.Error(t, err)

	// ExecuteToolCall 把错误包装成 observation 文本，而不是让引擎崩溃
	res := appkittools.ExecuteToolCall(context.Background(), r, "tc1", "generate_image", `{}`, nil)
	testx.True(t, res.IsError)
	testx.Contains(t, res.Result, "工具执行失败")
}

// TestExecuteToolCall_Hooks verifies OnBefore/OnAfter hooks fire around execution.
func TestExecuteToolCall_Hooks(t *testing.T) {
	r := appkittools.NewSubAgentToolRegistry()

	var after *appkittools.ToolCallResult
	gotBefore := false
	gotAfter := false
	hooks := &recordingHooks{
		onBefore: func(ctx context.Context, tcID, name, args string) {
			gotBefore = true
		},
		onAfter: func(ctx context.Context, res *appkittools.ToolCallResult) {
			gotAfter = true
			after = res
		},
	}

	res := appkittools.ExecuteToolCall(context.Background(), r, "tc1", "search", `{"query":"go"}`,
		hooks)
	testx.NotNil(t, res)
	testx.True(t, gotBefore)
	testx.True(t, gotAfter)
	testx.False(t, res.IsError)
	testx.NotNil(t, after)
	testx.Equal(t, res.Result, after.Result)
}

// TestMergeFunctions verifies strategy tools take priority on name collision.
func TestMergeFunctions(t *testing.T) {
	strategy := []appkittools.FunctionDef{
		{Name: "shared", Description: "strategy 版"},
		{Name: "only_strategy", Description: "s"},
	}
	providerTools := []appkittools.FunctionDef{
		{Name: "shared", Description: "provider 版（应被覆盖）"},
		{Name: "only_provider", Description: "p"},
	}

	merged := appkittools.MergeFunctions(strategy, providerTools)
	testx.Len(t, merged, 3)

	names := map[string]string{}
	for _, m := range merged {
		names[m.Name] = m.Description
	}
	testx.Equal(t, "strategy 版", names["shared"], "同名时 strategy 优先")
	testx.Contains(t, names, "only_provider")
}

type recordingHooks struct {
	onBefore func(ctx context.Context, tcID, name, args string)
	onAfter  func(ctx context.Context, res *appkittools.ToolCallResult)
}

func (h *recordingHooks) OnBefore(ctx context.Context, tcID, name, args string) {
	if h.onBefore != nil {
		h.onBefore(ctx, tcID, name, args)
	}
}

func (h *recordingHooks) OnAfter(ctx context.Context, res *appkittools.ToolCallResult) {
	if h.onAfter != nil {
		h.onAfter(ctx, res)
	}
}

// TestIdentityFromContext 验证身份注入与读取往返。
func TestIdentityFromContext(t *testing.T) {
	ctx := appkittools.WithIdentity(context.Background(), 7, "s1")
	uid, sid := appkittools.IdentityFromContext(ctx)
	testx.Equal(t, int64(7), uid)
	testx.Equal(t, "s1", sid)

	// 无注入的 ctx 返回零值
	uid2, sid2 := appkittools.IdentityFromContext(context.Background())
	testx.Equal(t, int64(0), uid2)
	testx.Equal(t, "", sid2)
}

// TestCreateVideoTaskReal_IdentityFromCtx 验证工具 Runner 只从 ctx 读身份传给 base-server。
// 即使工具参数带不同 user_id/session_id，也以 ctx 为准（身份不可由 LLM 参数注入）。
func TestCreateVideoTaskReal_IdentityFromCtx(t *testing.T) {
	// mock base-server：记录收到的 pre_create body
	var gotPreBody map[string]any
	baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/inner/creation/pre_create":
			_ = json.NewDecoder(r.Body).Decode(&gotPreBody)
			w.Write([]byte(`{"errno":0,"data":{"status":0,"task_type":"push_card","task_arguments":{},"project_input":"eyJpbnB1dCI6InRlc3QifQ=="}}`))
		case "/inner/creation/create":
			w.Write([]byte(`{"errno":0,"data":{"status":"success","task_type":"push_card","task_arguments":{}}}`))
		case "/inner/creation/async_post_create":
			w.Write([]byte(`{"errno":0,"data":{"status":"success","task_type":"push_card","task_arguments":{}}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer baseSrv.Close()

	fakegenSrv := fakegen.NewServer(fakegen.Config{
		DelayRange: fakegen.DelayRange{Min: 20 * time.Millisecond, Max: 20 * time.Millisecond},
	})
	defer fakegenSrv.Close()
	go fakegenSrv.Start(context.Background())

	baseClient := baseapi.NewClient(baseapi.ClientConfig{BaseURL: baseSrv.URL})
	r := appkittools.NewSubAgentToolRegistry(&appkittools.RealRunnerDeps{
		Base:            baseClient,
		FakeGen:         fakegenSrv,
		CallbackBaseURL: "http://go-learn.example",
	})

	// 注入身份的 ctx
	ctx := appkittools.WithIdentity(context.Background(), 7, "s1")

	// 参数不带身份 → 从 ctx 读
	_, _, _, err := r.Execute(ctx, "create_video_task", `{"prompt":"风景"}`)
	testx.NoError(t, err)
	testx.Equal(t, float64(7), gotPreBody["user_id"])
	testx.Equal(t, "s1", gotPreBody["session_id"])

	// 参数带不同身份 → 仍以 ctx 为准（参数被忽略，防 LLM 注入）
	_, _, _, err = r.Execute(ctx, "create_video_task", `{"prompt":"风景","user_id":999,"session_id":"spoofed"}`)
	testx.NoError(t, err)
	testx.Equal(t, float64(7), gotPreBody["user_id"], "参数 user_id 应被忽略，ctx 胜出")
	testx.Equal(t, "s1", gotPreBody["session_id"], "参数 session_id 应被忽略，ctx 胜出")
}

// TestNewSubAgentToolRegistry_WithFakeGen 验证注入依赖后 create_video_task 走真实四连。
func TestNewSubAgentToolRegistry_WithFakeGen(t *testing.T) {
	// mock base-server（记录收到哪几个接口）
	// hits 会被 fakegen 回调 goroutine（20ms 延迟）并发 append，用 mutex 保护避免数据竞争。
	var hitsMu sync.Mutex
	var hits []string
	baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsMu.Lock()
		hits = append(hits, r.URL.Path)
		hitsMu.Unlock()
		switch r.URL.Path {
		case "/inner/creation/pre_create":
			w.Write([]byte(`{"errno":0,"data":{"status":0,"task_type":"push_card","task_arguments":{},"project_input":"eyJpbnB1dCI6InRlc3QifQ=="}}`))
		case "/inner/creation/create":
			w.Write([]byte(`{"errno":0,"data":{"status":"success","task_type":"push_card","task_arguments":{}}}`))
		case "/inner/creation/async_post_create":
			w.Write([]byte(`{"errno":0,"data":{"status":"success","task_type":"push_card","task_arguments":{}}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer baseSrv.Close()

	// fakegen：固定延迟 20ms 快速回调
	fakegenSrv := fakegen.NewServer(fakegen.Config{
		DelayRange: fakegen.DelayRange{Min: 20 * time.Millisecond, Max: 20 * time.Millisecond},
	})
	defer fakegenSrv.Close()
	go fakegenSrv.Start(context.Background())

	baseClient := baseapi.NewClient(baseapi.ClientConfig{BaseURL: baseSrv.URL})
	r := appkittools.NewSubAgentToolRegistry(&appkittools.RealRunnerDeps{
		Base:            baseClient,
		FakeGen:         fakegenSrv,
		CallbackBaseURL: "http://go-learn.example", // 回调入口在 go-learn 自身
	})

	out, handled, isPipeline, err := r.Execute(context.Background(), "create_video_task", `{"prompt":"风景"}`)
	testx.NoError(t, err)
	testx.True(t, handled)
	testx.True(t, isPipeline, "create_video_task 是产线工具")
	// 返回 fakegen 原子分配的产线 task_id
	var res struct {
		PipelineTaskID int64  `json:"pipeline_task_id"`
		Status         string `json:"status"`
	}
	testx.NoError(t, json.Unmarshal([]byte(out), &res))
	testx.Greater(t, res.PipelineTaskID, int64(0), "task_id 应由 fakegen 原子分配（>0）")
	testx.Equal(t, "submitted", res.Status)

	hitsMu.Lock()
	defer hitsMu.Unlock()
	testx.Contains(t, hits, "/inner/creation/pre_create", "应调 pre_create")
	testx.Contains(t, hits, "/inner/creation/create", "应调 create")
	testx.Contains(t, hits, "/inner/creation/async_post_create", "应调 async_post_create")
}

// TestNewSubAgentToolRegistry_NilFallback 验证无注入时回退假实现（demo 可跑）。
func TestNewSubAgentToolRegistry_NilFallback(t *testing.T) {
	r := appkittools.NewSubAgentToolRegistry()
	out, handled, isPipeline, err := r.Execute(context.Background(), "create_video_task", `{"prompt":"风景"}`)
	testx.NoError(t, err)
	testx.True(t, handled)
	testx.True(t, isPipeline)
	testx.Contains(t, out, "submitted")
}

// TestNewSubAgentToolRegistry_RealStatusBranches 覆盖真实 Runner 的 status 分支：
// pre_create status!=0（失败卡/风控）、create insufficient_score、Data==nil。
func TestNewSubAgentToolRegistry_RealStatusBranches(t *testing.T) {
	t.Run("pre_create 失败卡", func(t *testing.T) {
		baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/inner/creation/pre_create":
				// status!=0 且 task_arguments.card_type 标识卡类型
				w.Write([]byte(`{"errno":0,"data":{"status":100,"task_type":"card","task_arguments":{"card_type":"input_risk"}}}`))
			default:
				w.WriteHeader(404)
			}
		}))
		defer baseSrv.Close()
		r := appkittools.NewSubAgentToolRegistry(&appkittools.RealRunnerDeps{
			Base: baseapi.NewClient(baseapi.ClientConfig{BaseURL: baseSrv.URL}),
		})
		out, handled, isPipeline, err := r.Execute(context.Background(), "create_video_task", `{"prompt":"风景"}`)
		testx.NoError(t, err)
		testx.True(t, handled)
		testx.False(t, isPipeline, "失败卡不进入生成，不应 isPipeline")
		var res struct {
			Status   int    `json:"status"`
			TaskType string `json:"task_type"`
			CardType string `json:"card_type"`
		}
		testx.NoError(t, json.Unmarshal([]byte(out), &res))
		testx.Equal(t, 100, res.Status)
		testx.Equal(t, "card", res.TaskType)
		testx.Equal(t, "input_risk", res.CardType, "应带出 card_type 区分卡类型")
	})

	t.Run("pre_create 风控拦截", func(t *testing.T) {
		baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/inner/creation/pre_create":
				w.Write([]byte(`{"errno":0,"data":{"status":200,"task_type":"card","task_arguments":{}}}`))
			default:
				w.WriteHeader(404)
			}
		}))
		defer baseSrv.Close()
		r := appkittools.NewSubAgentToolRegistry(&appkittools.RealRunnerDeps{
			Base: baseapi.NewClient(baseapi.ClientConfig{BaseURL: baseSrv.URL}),
		})
		out, handled, isPipeline, err := r.Execute(context.Background(), "create_video_task", `{"prompt":"风景"}`)
		testx.NoError(t, err)
		testx.True(t, handled)
		testx.False(t, isPipeline, "风控拦截不进入生成，不应 isPipeline")
		testx.Contains(t, out, `"status":200`)
	})

	t.Run("create 积分不足", func(t *testing.T) {
		baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/inner/creation/pre_create":
				w.Write([]byte(`{"errno":0,"data":{"status":0,"task_type":"push_card","task_arguments":{},"project_input":"eyJpbnB1dCI6InRlc3QifQ=="}}`))
			case "/inner/creation/create":
				// 积分不足：status=insufficient_score
				w.Write([]byte(`{"errno":0,"data":{"status":"insufficient_score","task_type":"card","task_arguments":{}}}`))
			default:
				w.WriteHeader(404)
			}
		}))
		defer baseSrv.Close()
		r := appkittools.NewSubAgentToolRegistry(&appkittools.RealRunnerDeps{
			Base: baseapi.NewClient(baseapi.ClientConfig{BaseURL: baseSrv.URL}),
		})
		out, handled, isPipeline, err := r.Execute(context.Background(), "create_video_task", `{"prompt":"风景"}`)
		testx.NoError(t, err)
		testx.True(t, handled)
		testx.False(t, isPipeline, "积分不足不进入生成，不应 isPipeline")
		testx.JSONEq(t, `{"status":"insufficient_score"}`, out)
	})

	t.Run("pre_create Data 为空", func(t *testing.T) {
		baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/inner/creation/pre_create":
				// errno=0 但 data 缺省：上游异常形态
				w.Write([]byte(`{"errno":0}`))
			default:
				w.WriteHeader(404)
			}
		}))
		defer baseSrv.Close()
		r := appkittools.NewSubAgentToolRegistry(&appkittools.RealRunnerDeps{
			Base: baseapi.NewClient(baseapi.ClientConfig{BaseURL: baseSrv.URL}),
		})
		_, handled, _, err := r.Execute(context.Background(), "create_video_task", `{"prompt":"风景"}`)
		testx.True(t, handled)
		testx.Error(t, err)
		testx.Contains(t, err.Error(), "pre_create: empty data")
	})
}

// TestToolRegistry_AgentIsolation 验证按 agent 隔离工具集：
// gold_scriptwriter 拿不到产线工具（create_video_task），edit_master 能拿到。
// 这是"写完脚本必走脚本卡"的工具级保证（对齐真实系统 GetFunctions 按 agentID 过滤）。
func TestToolRegistry_AgentIsolation(t *testing.T) {
	r := appkittools.NewSubAgentToolRegistry()
	ctx := context.Background()

	// 无 agentID 上下文：全局默认（search + generate_image，无产线工具）
	defs, err := r.GetFunctionDefs(ctx)
	testx.NoError(t, err)
	var names []string
	for _, d := range defs {
		names = append(names, d.Name)
	}
	testx.NotContains(t, names, "create_video_task", "全局默认不应有产线工具")
	testx.Contains(t, names, "search")

	// gold_scriptwriter：写脚本，看不到产线工具
	swCtx := appkittools.WithAgentID(ctx, appkittools.AgentGoldScriptwriter)
	swDefs, err := r.GetFunctionDefs(swCtx)
	testx.NoError(t, err)
	var swNames []string
	for _, d := range swDefs {
		swNames = append(swNames, d.Name)
	}
	testx.NotContains(t, swNames, "create_video_task", "gold_scriptwriter 不应看到产线工具")
	testx.Contains(t, swNames, "search")
	testx.Contains(t, swNames, "generate_image")

	// edit_master：剪视频，能看到产线工具
	emCtx := appkittools.WithAgentID(ctx, appkittools.AgentEditMaster)
	emDefs, err := r.GetFunctionDefs(emCtx)
	testx.NoError(t, err)
	var emNames []string
	for _, d := range emDefs {
		emNames = append(emNames, d.Name)
	}
	testx.Contains(t, emNames, "create_video_task", "edit_master 应有产线工具")
	testx.Contains(t, emNames, "search")
}
