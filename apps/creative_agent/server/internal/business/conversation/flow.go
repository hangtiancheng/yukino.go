package conversation

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/fastpath"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/ops"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask/taskflow"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/sse"
)

// FCKGdpCtx 是 flow context 中 HTTP body 的 key（re-export 自 ops）。
const FCKGdpCtx = ops.FCKGdpCtx

// FlowDeps 是 /aigc/conversation 端点所需的依赖。
type FlowDeps struct {
	SessionRepo       repository.SessionRepository
	MessageRecordRepo repository.MessageRecordRepository

	// Phase 2 async pipeline
	AsyncRepo    repository.AsyncRequestRepository
	ChunkStore   infra.ChunkStore
	StreamQ      infra.StreamQueue
	Locker       infra.Locker
	AsyncEnabled bool

	// Phase 4: 快捷路径（nil 时跳过 FastPath 判断）
	FastPath *fastpath.FastPath

	// Phase 4: 同步快任务回流（nil 时快路径命中只推完成帧）
	TaskPoller  *taskflow.Poller
	TaskRunRepo repository.TaskRunRepository
}

// NewFlow 注册算子并返回 conversation flow 图（业务层入口方法）。
// handler 只负责 HTTP 绑定，把 body 注入 flow context 后调用 WaitExec。
func NewFlow(w *sse.Writer, deps FlowDeps) seda.FlowConf {
	seda.Register("con_reader", func() seda.Operator {
		return ops.NewConReaderNode(
			deps.SessionRepo, deps.MessageRecordRepo,
			deps.AsyncRepo, deps.StreamQ, deps.Locker, deps.AsyncEnabled,
		)
	})
	seda.Register("supervisor_agent", func() seda.Operator {
		return ops.NewSupervisorAgentNode(deps.ChunkStore, deps.AsyncRepo, deps.FastPath, deps.TaskPoller, deps.TaskRunRepo)
	})
	seda.Register("con_writer", func() seda.Operator {
		return ops.NewConWriterNode(w)
	})
	seda.Register("con_end", func() seda.Operator {
		return ops.NewEndNode()
	})
	return seda.FlowConf{
		Name:  "conversation",
		Ops:   []string{"con_reader", "supervisor_agent", "con_writer", "con_end"},
		Start: "con_reader",
		Last:  "con_end",
		Edges: []seda.Edge{
			{From: "con_reader", To: "supervisor_agent"},
			{From: "con_reader", To: "con_writer"},
			{From: "supervisor_agent", To: "con_end"},
			{From: "con_writer", To: "con_end"},
		},
	}
}
