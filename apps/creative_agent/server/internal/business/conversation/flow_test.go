package conversation_test

import (
	"testing"

	conversation "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestNewFlowGraphShape validates the static structure of the conversation flow graph.
// The graph definition is the foundation of runtime correctness; Start/Last/Ops/Edges are locked here,
// preventing operator-name or edge typos from slipping past compile time.
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

	// Smoke: all four operators are registered in the registry; BuildFlow does not panic.
	testx.NotPanics(t, func() { seda.BuildFlow(conf) })
}
