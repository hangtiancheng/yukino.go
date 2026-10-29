package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appkitrunner "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/runner"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/fastpath"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/worker"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/taskflow"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"
)

// ConOnePackSSEKey is the flow-context key of sync SSE frames.
// The supervisor pushes frames here; con_writer consumes them.
const ConOnePackSSEKey = "con_text_sse"

// ConReconnectSSEKey is the flow-context key of async/reconnect SSE frames.
// RunAttach pushes frames here; con_writer consumes them.
const ConReconnectSSEKey = "con_reconnect_sse"

// SSEFrame is an alias for appkitrunner.SSEFrame used in operator callbacks.
type SSEFrame = appkitrunner.SSEFrame

// SupervisorAgentNode is the business operator running the ReAct loop.
//
// Phase 1 (sync): DoImpl calls runSupervisorBusiness directly → ReAct executes → Push ConOnePackSSEKey.
// Phase 2 (async): when asyncShortCircuit=true exists in the flow context, DoImpl switches to doAttach,
//
//	pulling backflow data from the Redis chunk_store (RunAttach) and pushing via ConReconnectSSEKey.
//
// Sync and async paths reuse the same SupervisorRunner.Run engine; only the onFrame callback target differs.
type SupervisorAgentNode struct {
	// chunkStore is the cross-process relay store: Workers write chunks into it; RunAttach reads from it.
	// When nil the async branch is unavailable (Phase 1 / async disabled).
	chunkStore infra.ChunkStore
	// asyncRepo is used to poll the async task's terminal state when attach sees no new chunks (whether the Worker died).
	asyncRepo repository.AsyncRequestRepository

	// Phase 4: fast path (when nil, the FastPath check is skipped and ReAct runs).
	fastPath *fastpath.FastPath

	// Phase 4: sync fast-task backflow — after dispatching subtasks, poll cg_task_step synchronously and push frames.
	// When nil, a fast-path hit only pushes the completion frame.
	taskPoller  *taskflow.Poller
	taskRunRepo repository.TaskRunRepository
}

// NewSupervisorAgentNode creates a SupervisorAgentNode.
func NewSupervisorAgentNode(chunkStore infra.ChunkStore, asyncRepo repository.AsyncRequestRepository,
	fastPath *fastpath.FastPath, taskPoller *taskflow.Poller, taskRunRepo repository.TaskRunRepository) *SupervisorAgentNode {
	return &SupervisorAgentNode{
		chunkStore:  chunkStore,
		asyncRepo:   asyncRepo,
		fastPath:    fastPath,
		taskPoller:  taskPoller,
		taskRunRepo: taskRunRepo,
	}
}

// Name returns the operator name.
func (n *SupervisorAgentNode) Name() string { return "supervisor_agent" }

// DoImpl runs the ReAct supervisor business logic.
// Phase 2: check asyncShortCircuit → switch to RunAttach in async mode.
// Phase 4: the sync path injects FastPath; on a fast-path hit it pushes the completion frame directly without running ReAct.
func (n *SupervisorAgentNode) DoImpl(ctx *seda.Context) (int, string) {
	// Async short-circuit branch
	if shortCircuit, _ := ctx.Get("asyncShortCircuit").(bool); shortCircuit {
		return n.doAttach(ctx)
	}

	query, _ := ctx.Get(FCKQuery).(string)
	serverMsgID, _ := ctx.Get(FCKServerMsgID).(string)

	// Phase 4: fast path (HIL resume / Retry / CanFastTask)
	// seda.Context has no GetString/GetInt64/GetBool conveniences; values are fetched with type assertions.
	// con_reader guarantees these keys are the target types in writeOutput; missing keys are treated as zero values.
	// On internal CheckFastTask errors handled=false → silently fall back to ReAct (the sync path is not blocked).
	if n.fastPath != nil {
		sessionID, _ := ctx.Get(FCKSessionID).(string)
		userID, _ := ctx.Get(FCKUserID).(int64)
		agentID, _ := ctx.Get(FCKAgentID).(string)
		requestType, _ := ctx.Get(FCKRequestType).(string)
		resume, _ := ctx.Get(FCKResume).(bool)

		// Use the HTTP span restored from the flow context as ctx: when dispatching subtasks, Submitter.Submit writes
		// the worker's traceparent into the task stream so the fast-path task.process hangs under the same trace.
		fctx := tracex.ContextFromFlow(ctx)
		originMsgID, _ := ctx.Get(FCKOriginMsgID).(string)
		handled, _ := n.fastPath.CheckFastTask(fctx, &fastpath.FastTaskRequest{
			SessionID:   sessionID,
			UserID:      userID,
			AgentID:     agentID,
			Query:       query,
			RequestType: requestType,
			Resume:      resume,
			MsgID:       serverMsgID,
			OriginMsgID: originMsgID,
		})
		if handled {
			// Sync resume: hit but no checkpoint pre-loading mechanism (the full resume lives in the async Worker Task 8);
			// only the completion frame is pushed here.
			if resume {
				n.pushFlowEnd(ctx)
			} else {
				// CanFastTask/Retry: subtasks dispatched → synchronously poll cg_task_step for progress frames
				n.pollTaskAndFinish(ctx, serverMsgID)
			}
			return 0, ""
		}
	}

	n.runSupervisorBusiness(ctx, query, serverMsgID)
	return 0, ""
}

// pushFlowEnd tells con_writer the stream ended (when a fast-path hit produced no SSE frames).
func (n *SupervisorAgentNode) pushFlowEnd(ctx *seda.Context) {
	ctx.Push(ConOnePackSSEKey, "__FLOW_END__")
}

// pollTaskAndFinish runs after a fast-path hit (subtasks dispatched): synchronously poll cg_task_step and push task-step/task-done backflow frames.
// It reuses taskflow.Poller (isomorphic to the async Worker's PollUntilDone); frame JSON is pushed directly to ConOnePackSSEKey.
func (n *SupervisorAgentNode) pollTaskAndFinish(ctx *seda.Context, serverMsgID string) {
	if n.taskPoller == nil || n.taskRunRepo == nil {
		n.pushFlowEnd(ctx)
		return
	}
	// Look up this dispatch's task_run (the latest one associated with the msgID)
	run, err := n.taskRunRepo.GetByMsgID(context.Background(), serverMsgID)
	if err != nil || run == nil {
		n.pushFlowEnd(ctx)
		return
	}
	// The polling ctx derives from the HTTP span restored from the flow: task_step/task_run queries hang under the HTTP trace.
	pollCtx, cancel := context.WithTimeout(tracex.ContextFromFlow(ctx), 60*time.Second)
	defer cancel()
	n.taskPoller.PollUntilDone(pollCtx, run.TaskRunID, serverMsgID, func(f taskflow.TaskStepFrame) {
		ctx.Push(ConOnePackSSEKey, taskflow.FrameToJSON(f))
	}, 500*time.Millisecond)
	n.pushFlowEnd(ctx)
}

// doAttach is the async path: it does not run ReAct; instead it pulls backflow data from the Redis chunk_store.
func (n *SupervisorAgentNode) doAttach(ctx *seda.Context) (int, string) {
	msgID, _ := ctx.Get(FCKServerMsgID).(string)
	if msgID == "" {
		return -1, "supervisor_agent: async short circuit but no serverMsgId"
	}
	if n.chunkStore == nil {
		return -1, "supervisor_agent: async short circuit but chunkStore is nil"
	}

	// lastSeq resumes from the breakpoint (chunks already consumed by the frontend); 0 for the first time.
	lastSeq, _ := ctx.Get("lastSeq").(int64)

	// RunAttach blocks until EndTurn / terminal state / ctx cancellation.
	// Use the HTTP span restored from the flow as ctx: attach's DB polling / chunk reads hang under the same trace.
	if err := worker.RunAttach(tracex.ContextFromFlow(ctx), msgID, n.chunkStore, ctx, ConReconnectSSEKey, lastSeq, n.asyncRepo); err != nil {
		return -1, fmt.Sprintf("supervisor_agent: attach failed: %v", err)
	}
	return 0, ""
}

func (n *SupervisorAgentNode) runSupervisorBusiness(ctx *seda.Context, query, serverMsgID string) {
	runner := appkitrunner.NewRunner()

	// Use the HTTP span restored from the flow context as the parent span so supervisor.react hangs under the same trace.
	_, err := runner.Run(tracex.ContextFromFlow(ctx), query, serverMsgID, func(frame appkitrunner.SSEFrame) {
		pushFrame(ctx, frame)
	})
	if err != nil {
		// Error was already pushed as SSEFrame by runner.Run
		return
	}

	// Send the flow-end sentinel so con_writer does not wait forever for con_end
	ctx.Push(ConOnePackSSEKey, "__FLOW_END__")
}

func pushFrame(ctx *seda.Context, frame SSEFrame) {
	b, _ := json.Marshal(frame)
	ctx.Push(ConOnePackSSEKey, string(b))
}
