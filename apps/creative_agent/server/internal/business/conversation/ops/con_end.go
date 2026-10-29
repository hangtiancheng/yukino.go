package ops

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"

// EndNode is the terminal operator of the conversation flow;
// it pushes the flow-end sentinel to tell con_writer to exit.
type EndNode struct{}

// NewEndNode creates an EndNode.
func NewEndNode() *EndNode { return &EndNode{} }

// Name returns the operator name.
func (e *EndNode) Name() string { return "con_end" }

// DoImpl pushes the __FLOW_END__ sentinel to both SSE keys,
// telling con_writer's two listening goroutines to exit.
func (e *EndNode) DoImpl(ctx *seda.Context) (int, string) {
	ctx.Push(ConOnePackSSEKey, "__FLOW_END__")
	ctx.Push(ConReconnectSSEKey, "__FLOW_END__")
	return 0, ""
}
