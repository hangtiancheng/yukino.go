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

// ConOnePackSSEKey 是同步 SSE 帧的 flow context key。
// supervisor 往这里推帧，con_writer 从这里消费。
const ConOnePackSSEKey = "con_text_sse"

// ConReconnectSSEKey 是异步/重连 SSE 帧的 flow context key。
// RunAttach 往这里推帧，con_writer 从这里消费。
const ConReconnectSSEKey = "con_reconnect_sse"

// SSEFrame is an alias for appkitrunner.SSEFrame used in operator callbacks.
type SSEFrame = appkitrunner.SSEFrame

// SupervisorAgentNode 是运行 ReAct 循环的业务算子。
//
// Phase 1（同步）：DoImpl 直接调用 runSupervisorBusiness → ReAct 执行 → Push ConOnePackSSEKey。
// Phase 2（异步）：当 flowcontext 中存在 asyncShortCircuit=true 时，DoImpl 转 doAttach，
//
//	改从 Redis chunk_store 拉回流数据（RunAttach），通过 ConReconnectSSEKey 推送。
//
// 同步和异步路径复用同一个 SupervisorRunner.Run 引擎，差异仅 onFrame 回调目标不同。
type SupervisorAgentNode struct {
	// chunkStore 是跨进程中转站：Worker 往里写 chunk，RunAttach 从里读。
	// nil 时异步分支不可用（Phase 1 / 异步未启用时）。
	chunkStore infra.ChunkStore
	// asyncRepo 用于 attach 无新 chunk 时轮询异步任务终态（Worker 是否已死）。
	asyncRepo repository.AsyncRequestRepository

	// Phase 4: 快捷路径（nil 时跳过 FastPath 判断，走 ReAct）。
	fastPath *fastpath.FastPath

	// Phase 4: 同步快任务回流——子任务投递后同步轮询 cg_task_step 推帧。
	// nil 时快路径命中只推完成帧。
	taskPoller  *taskflow.Poller
	taskRunRepo repository.TaskRunRepository
}

// NewSupervisorAgentNode 创建 SupervisorAgentNode。
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

// Name 返回算子名。
func (n *SupervisorAgentNode) Name() string { return "supervisor_agent" }

// DoImpl 跑 ReAct supervisor 业务逻辑。
// Phase 2: 检查 asyncShortCircuit → 若是异步模式则转入 RunAttach。
// Phase 4: 同步路径注入 FastPath，命中快路径时直接推送完成帧，不跑 ReAct。
func (n *SupervisorAgentNode) DoImpl(ctx *seda.Context) (int, string) {
	// 异步短路分支
	if shortCircuit, _ := ctx.Get("asyncShortCircuit").(bool); shortCircuit {
		return n.doAttach(ctx)
	}

	query, _ := ctx.Get(FCKQuery).(string)
	serverMsgID, _ := ctx.Get(FCKServerMsgID).(string)

	// Phase 4: 快捷路径（HIL resume / Retry / CanFastTask）
	// seda.Context 无 GetString/GetInt64/GetBool 便捷方法，用类型断言取值。
	// con_reader 在 writeOutput 已保证这些 key 均为目标类型，缺失时按零值处理。
	// CheckFastTask 内部错误时 handled=false → 静默回退 ReAct（不阻断同步路径）。
	if n.fastPath != nil {
		sessionID, _ := ctx.Get(FCKSessionID).(string)
		userID, _ := ctx.Get(FCKUserID).(int64)
		agentID, _ := ctx.Get(FCKAgentID).(string)
		requestType, _ := ctx.Get(FCKRequestType).(string)
		resume, _ := ctx.Get(FCKResume).(bool)

		// 用 flow context 恢复的 HTTP span 作为 ctx：投递子任务时 Submitter.Submit 会把
		// worker 的 traceparent 写入 task stream，让 fast-path 的 task.process 挂进同一条 trace。
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
			// 同步 resume：命中但无 checkpoint 预置机制（完整 resume 在异步 Worker Task 8），
			// 此处只推完成帧。
			if resume {
				n.pushFlowEnd(ctx)
			} else {
				// CanFastTask/Retry：已投递子任务 → 同步轮询 cg_task_step 回流进度帧
				n.pollTaskAndFinish(ctx, serverMsgID)
			}
			return 0, ""
		}
	}

	n.runSupervisorBusiness(ctx, query, serverMsgID)
	return 0, ""
}

// pushFlowEnd 通知 con_writer 流结束（快路径命中后无 SSE 帧时）。
func (n *SupervisorAgentNode) pushFlowEnd(ctx *seda.Context) {
	ctx.Push(ConOnePackSSEKey, "__FLOW_END__")
}

// pollTaskAndFinish 快路径命中（已投递子任务）后：同步轮询 cg_task_step 推 task-step/task-done 帧回流。
// 复用 taskflow.Poller（与异步 Worker 的 PollUntilDone 同构），帧 JSON 直接 push 到 ConOnePackSSEKey。
func (n *SupervisorAgentNode) pollTaskAndFinish(ctx *seda.Context, serverMsgID string) {
	if n.taskPoller == nil || n.taskRunRepo == nil {
		n.pushFlowEnd(ctx)
		return
	}
	// 查本次投递的 task_run（该 msgID 关联的最新一条）
	run, err := n.taskRunRepo.GetByMsgID(context.Background(), serverMsgID)
	if err != nil || run == nil {
		n.pushFlowEnd(ctx)
		return
	}
	// 轮询 ctx 从 flow 恢复的 HTTP span 派生：task_step/task_run 查询挂进 HTTP trace。
	pollCtx, cancel := context.WithTimeout(tracex.ContextFromFlow(ctx), 60*time.Second)
	defer cancel()
	n.taskPoller.PollUntilDone(pollCtx, run.TaskRunID, serverMsgID, func(f taskflow.TaskStepFrame) {
		ctx.Push(ConOnePackSSEKey, taskflow.FrameToJSON(f))
	}, 500*time.Millisecond)
	n.pushFlowEnd(ctx)
}

// doAttach 是异步路径：不跑 ReAct，改从 Redis chunk_store 拉回流数据。
func (n *SupervisorAgentNode) doAttach(ctx *seda.Context) (int, string) {
	msgID, _ := ctx.Get(FCKServerMsgID).(string)
	if msgID == "" {
		return -1, "supervisor_agent: async short circuit but no serverMsgId"
	}
	if n.chunkStore == nil {
		return -1, "supervisor_agent: async short circuit but chunkStore is nil"
	}

	// lastSeq 从断点续拉（前端已消费的 chunk 数），首次为 0。
	lastSeq, _ := ctx.Get("lastSeq").(int64)

	// RunAttach 会阻塞直到 EndTurn / 终态 / ctx 取消
	// 用 flow 恢复的 HTTP span 作为 ctx：attach 的 DB 轮询 / chunk 读取挂进同一条 trace。
	if err := worker.RunAttach(tracex.ContextFromFlow(ctx), msgID, n.chunkStore, ctx, ConReconnectSSEKey, lastSeq, n.asyncRepo); err != nil {
		return -1, fmt.Sprintf("supervisor_agent: attach failed: %v", err)
	}
	return 0, ""
}

func (n *SupervisorAgentNode) runSupervisorBusiness(ctx *seda.Context, query, serverMsgID string) {
	runner := appkitrunner.NewRunner()

	// 用 flow context 恢复的 HTTP span 作为父 span，把 supervisor.react 挂进同一条 trace。
	_, err := runner.Run(tracex.ContextFromFlow(ctx), query, serverMsgID, func(frame appkitrunner.SSEFrame) {
		pushFrame(ctx, frame)
	})
	if err != nil {
		// Error was already pushed as SSEFrame by runner.Run
		return
	}

	// 发送流结束哨兵，避免 con_writer 死等 con_end
	ctx.Push(ConOnePackSSEKey, "__FLOW_END__")
}

func pushFrame(ctx *seda.Context, frame SSEFrame) {
	b, _ := json.Marshal(frame)
	ctx.Push(ConOnePackSSEKey, string(b))
}
