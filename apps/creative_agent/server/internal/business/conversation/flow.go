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

// FCKGdpCtx is the flow-context key of the HTTP body (re-exported from ops).
const FCKGdpCtx = ops.FCKGdpCtx

// FlowDeps holds the dependencies required by the /aigc/conversation endpoint.
type FlowDeps struct {
	SessionRepo       repository.SessionRepository
	MessageRecordRepo repository.MessageRecordRepository

	// Phase 2 async pipeline
	AsyncRepo    repository.AsyncRequestRepository
	ChunkStore   infra.ChunkStore
	StreamQ      infra.StreamQueue
	Locker       infra.Locker
	AsyncEnabled bool

	// Phase 4: fast path (the FastPath check is skipped when nil)
	FastPath *fastpath.FastPath

	// Phase 4: sync fast-task backflow (when nil, a fast-path hit only pushes the completion frame)
	TaskPoller  *taskflow.Poller
	TaskRunRepo repository.TaskRunRepository
}

// NewFlow registers the operators and returns the conversation flow graph (the business-layer entry method).
// The handler only does HTTP binding: it injects the body into the flow context and calls WaitExec.
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
