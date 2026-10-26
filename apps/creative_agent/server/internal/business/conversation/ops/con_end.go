package ops

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"

// EndNode 是 conversation flow 的终结算子，
// 推送流结束哨兵通知 con_writer 退出。
type EndNode struct{}

// NewEndNode 创建 EndNode。
func NewEndNode() *EndNode { return &EndNode{} }

// Name 返回算子名。
func (e *EndNode) Name() string { return "con_end" }

// DoImpl 推送 __FLOW_END__ 哨兵到两个 SSE key，
// 通知 con_writer 的两个监听 goroutine 退出。
func (e *EndNode) DoImpl(ctx *seda.Context) (int, string) {
	ctx.Push(ConOnePackSSEKey, "__FLOW_END__")
	ctx.Push(ConReconnectSSEKey, "__FLOW_END__")
	return 0, ""
}
