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
	testx.NotEmpty(t, defs[0].Description, "description decides when the LLM picks the tool; it is required")
	testx.Equal(t, "object", defs[0].Parameters.Type)
	testx.Contains(t, defs[0].Parameters.Required, "query")
}

// TestToolRegistry_Execute verifies Execute dispatches to the right runner.
func TestToolRegistry_Execute(t *testing.T) {
	r := appkittools.NewSubAgentToolRegistry()

	// Hit: sync tool
	out, handled, isPipeline, err := r.Execute(context.Background(), "generate_image", `{"prompt":"一只猫"}`)
	testx.NoError(t, err)
	testx.True(t, handled)
	testx.False(t, isPipeline)
	var img struct {
		URL string `json:"url"`
	}
	testx.NoError(t, json.Unmarshal([]byte(out), &img))
	testx.Contains(t, img.URL, "https://img.example.com/gen/")

	// Pipeline tool: isPipeline=true (triggers NeedWait branching)
	out2, handled2, isPipeline2, err := r.Execute(context.Background(), "create_video_task", `{"prompt":"风景"}`)
	testx.NoError(t, err)
	testx.True(t, handled2)
	testx.True(t, isPipeline2, "create_video_task is a pipeline tool")
	testx.Contains(t, out2, "submitted")

	// Unregistered: handled=false
	_, handled3, _, err := r.Execute(context.Background(), "no_such_tool", `{}`)
	testx.False(t, handled3)
	testx.Error(t, err)
}

// TestToolRegistry_ExecuteErrorAsObservation verifies tool errors do not interrupt
// the loop — they surface as error strings (observation).
func TestToolRegistry_ExecuteErrorAsObservation(t *testing.T) {
	r := appkittools.NewSubAgentToolRegistry()

	// Missing required argument → the runner errors
	_, handled, _, err := r.Execute(context.Background(), "generate_image", `{}`)
	testx.True(t, handled)
	testx.Error(t, err)

	// ExecuteToolCall wraps the error into observation text instead of crashing the engine
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
		{Name: "shared", Description: "strategy version"},
		{Name: "only_strategy", Description: "s"},
	}
	providerTools := []appkittools.FunctionDef{
		{Name: "shared", Description: "provider version (should be overridden)"},
		{Name: "only_provider", Description: "p"},
	}

	merged := appkittools.MergeFunctions(strategy, providerTools)
	testx.Len(t, merged, 3)

	names := map[string]string{}
	for _, m := range merged {
		names[m.Name] = m.Description
	}
	testx.Equal(t, "strategy version", names["shared"], "on name collisions strategy wins")
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

// TestIdentityFromContext verifies the identity injection/read round trip.
func TestIdentityFromContext(t *testing.T) {
	ctx := appkittools.WithIdentity(context.Background(), 7, "s1")
	uid, sid := appkittools.IdentityFromContext(ctx)
	testx.Equal(t, int64(7), uid)
	testx.Equal(t, "s1", sid)

	// A ctx without injection returns zero values
	uid2, sid2 := appkittools.IdentityFromContext(context.Background())
	testx.Equal(t, int64(0), uid2)
	testx.Equal(t, "", sid2)
}

// TestCreateVideoTaskReal_IdentityFromCtx verifies that the tool Runner reads identity only from ctx and passes it to base-server.
// Even when tool arguments carry different user_id/session_id, ctx wins (identity cannot be injected via LLM arguments).
func TestCreateVideoTaskReal_IdentityFromCtx(t *testing.T) {
	// mock base-server: records the received pre_create body
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

	// ctx with identity injected
	ctx := appkittools.WithIdentity(context.Background(), 7, "s1")

	// Arguments carry no identity → read from ctx
	_, _, _, err := r.Execute(ctx, "create_video_task", `{"prompt":"风景"}`)
	testx.NoError(t, err)
	testx.Equal(t, float64(7), gotPreBody["user_id"])
	testx.Equal(t, "s1", gotPreBody["session_id"])

	// Arguments carry a different identity → ctx still wins (arguments are ignored, preventing LLM injection)
	_, _, _, err = r.Execute(ctx, "create_video_task", `{"prompt":"风景","user_id":999,"session_id":"spoofed"}`)
	testx.NoError(t, err)
	testx.Equal(t, float64(7), gotPreBody["user_id"], "the user_id argument should be ignored; ctx wins")
	testx.Equal(t, "s1", gotPreBody["session_id"], "the session_id argument should be ignored; ctx wins")
}

// TestNewSubAgentToolRegistry_WithFakeGen verifies that with injected dependencies create_video_task runs the real four-call chain.
func TestNewSubAgentToolRegistry_WithFakeGen(t *testing.T) {
	// mock base-server (records which endpoints are hit)
	// hits is appended concurrently by the fakegen callback goroutine (20ms delay); a mutex protects it against data races.
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

	// fakegen: fast callback with a fixed 20ms delay
	fakegenSrv := fakegen.NewServer(fakegen.Config{
		DelayRange: fakegen.DelayRange{Min: 20 * time.Millisecond, Max: 20 * time.Millisecond},
	})
	defer fakegenSrv.Close()
	go fakegenSrv.Start(context.Background())

	baseClient := baseapi.NewClient(baseapi.ClientConfig{BaseURL: baseSrv.URL})
	r := appkittools.NewSubAgentToolRegistry(&appkittools.RealRunnerDeps{
		Base:            baseClient,
		FakeGen:         fakegenSrv,
		CallbackBaseURL: "http://go-learn.example", // the callback entry point is go-learn itself
	})

	out, handled, isPipeline, err := r.Execute(context.Background(), "create_video_task", `{"prompt":"风景"}`)
	testx.NoError(t, err)
	testx.True(t, handled)
	testx.True(t, isPipeline, "create_video_task is a pipeline tool")
	// Returns the pipeline task_id atomically assigned by fakegen
	var res struct {
		PipelineTaskID int64  `json:"pipeline_task_id"`
		Status         string `json:"status"`
	}
	testx.NoError(t, json.Unmarshal([]byte(out), &res))
	testx.Greater(t, res.PipelineTaskID, int64(0), "task_id should be atomically assigned by fakegen (>0)")
	testx.Equal(t, "submitted", res.Status)

	hitsMu.Lock()
	defer hitsMu.Unlock()
	testx.Contains(t, hits, "/inner/creation/pre_create", "pre_create should be called")
	testx.Contains(t, hits, "/inner/creation/create", "create should be called")
	testx.Contains(t, hits, "/inner/creation/async_post_create", "async_post_create should be called")
}

// TestNewSubAgentToolRegistry_NilFallback verifies the fallback to the fake implementation without injection (the demo still runs).
func TestNewSubAgentToolRegistry_NilFallback(t *testing.T) {
	r := appkittools.NewSubAgentToolRegistry()
	out, handled, isPipeline, err := r.Execute(context.Background(), "create_video_task", `{"prompt":"风景"}`)
	testx.NoError(t, err)
	testx.True(t, handled)
	testx.True(t, isPipeline)
	testx.Contains(t, out, "submitted")
}

// TestNewSubAgentToolRegistry_RealStatusBranches covers the real Runner's status branches:
// pre_create status!=0 (failure card / risk control), create insufficient_score, Data==nil.
func TestNewSubAgentToolRegistry_RealStatusBranches(t *testing.T) {
	t.Run("pre_create failure card", func(t *testing.T) {
		baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/inner/creation/pre_create":
				// status!=0 and task_arguments.card_type identifies the card type
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
		testx.False(t, isPipeline, "a failure card does not enter generation; isPipeline should be false")
		var res struct {
			Status   int    `json:"status"`
			TaskType string `json:"task_type"`
			CardType string `json:"card_type"`
		}
		testx.NoError(t, json.Unmarshal([]byte(out), &res))
		testx.Equal(t, 100, res.Status)
		testx.Equal(t, "card", res.TaskType)
		testx.Equal(t, "input_risk", res.CardType, "card_type should be carried to distinguish the card type")
	})

	t.Run("pre_create risk-control block", func(t *testing.T) {
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
		testx.False(t, isPipeline, "a risk-control block does not enter generation; isPipeline should be false")
		testx.Contains(t, out, `"status":200`)
	})

	t.Run("create insufficient points", func(t *testing.T) {
		baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/inner/creation/pre_create":
				w.Write([]byte(`{"errno":0,"data":{"status":0,"task_type":"push_card","task_arguments":{},"project_input":"eyJpbnB1dCI6InRlc3QifQ=="}}`))
			case "/inner/creation/create":
				// Insufficient points: status=insufficient_score
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
		testx.False(t, isPipeline, "insufficient points does not enter generation; isPipeline should be false")
		testx.JSONEq(t, `{"status":"insufficient_score"}`, out)
	})

	t.Run("pre_create empty Data", func(t *testing.T) {
		baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/inner/creation/pre_create":
				// errno=0 but data missing: malformed upstream response
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

// TestToolRegistry_AgentIsolation verifies per-agent tool-set isolation:
// gold_scriptwriter cannot get the pipeline tool (create_video_task); edit_master can.
// This is the tool-level guarantee that "after writing the script the script card must be shown" (aligned with the reference system's GetFunctions filtering by agentID).
func TestToolRegistry_AgentIsolation(t *testing.T) {
	r := appkittools.NewSubAgentToolRegistry()
	ctx := context.Background()

	// No agentID context: global defaults (search + generate_image, no pipeline tools)
	defs, err := r.GetFunctionDefs(ctx)
	testx.NoError(t, err)
	var names []string
	for _, d := range defs {
		names = append(names, d.Name)
	}
	testx.NotContains(t, names, "create_video_task", "global defaults should not include pipeline tools")
	testx.Contains(t, names, "search")

	// gold_scriptwriter: writes scripts, cannot see pipeline tools
	swCtx := appkittools.WithAgentID(ctx, appkittools.AgentGoldScriptwriter)
	swDefs, err := r.GetFunctionDefs(swCtx)
	testx.NoError(t, err)
	var swNames []string
	for _, d := range swDefs {
		swNames = append(swNames, d.Name)
	}
	testx.NotContains(t, swNames, "create_video_task", "gold_scriptwriter should not see pipeline tools")
	testx.Contains(t, swNames, "search")
	testx.Contains(t, swNames, "generate_image")

	// edit_master: edits videos, can see pipeline tools
	emCtx := appkittools.WithAgentID(ctx, appkittools.AgentEditMaster)
	emDefs, err := r.GetFunctionDefs(emCtx)
	testx.NoError(t, err)
	var emNames []string
	for _, d := range emDefs {
		emNames = append(emNames, d.Name)
	}
	testx.Contains(t, emNames, "create_video_task", "edit_master should have pipeline tools")
	testx.Contains(t, emNames, "search")
}
