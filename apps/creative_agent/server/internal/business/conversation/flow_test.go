package conversation_test

import (
	"testing"

	conversation "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestNewFlowGraphShape 校验 conversation flow 图的静态结构。
// 图定义是运行时正确性的根基，此处锁定 Start/Last/Ops/Edges，
// 防止算子名或边拼写错误在编译期漏网。
func TestNewFlowGraphShape(t *testing.T) {
	conf := conversation.NewFlow(nil, conversation.FlowDeps{})

	testx.Equal(t, "con_reader", conf.Start)
	testx.Equal(t, "con_end", conf.Last)
	testx.Equal(t, []string{"con_reader", "supervisor_agent", "con_writer", "con_end"}, conf.Ops)
	testx.ElementsMatch(t, []seda.Edge{
		{From: "con_reader", To: "supervisor_agent"},
		{From: "con_reader", To: "con_writer"},
		{From: "supervisor_agent", To: "con_end"},
		{From: "con_writer", To: "con_end"},
	}, conf.Edges)

	// 冒烟：四个算子都已在 registry 注册，BuildFlow 不 panic。
	testx.NotPanics(t, func() { seda.BuildFlow(conf) })
}
