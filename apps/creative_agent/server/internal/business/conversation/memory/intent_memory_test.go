package memory_test

import (
	"encoding/json"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestFlowStage_Valid verifies the FlowStage state constants and validity checks.
func TestFlowStage_Valid(t *testing.T) {
	// Valid values
	for _, s := range []memory.FlowStage{
		memory.FlowStageEmpty,
		memory.FlowStageTopicConfirmed,
		memory.FlowStageCategoryConfirmed,
		memory.FlowStageParamsConfirmed,
		memory.FlowStagePlanConfirmed,
		memory.FlowStageScriptConfirmed,
	} {
		testx.True(t, s.Valid(), "%q should be a valid FlowStage", s)
	}
	// Invalid values
	testx.False(t, memory.FlowStage("bogus").Valid())
}

// TestFlowStage_CanFastTask verifies the CanFastTask decision:
// params/plan/script confirmed → a fast task can trigger; early stages / not advanced → no.
func TestFlowStage_CanFastTask(t *testing.T) {
	testx.False(t, memory.FlowStageEmpty.CanFastTask())
	testx.False(t, memory.FlowStageTopicConfirmed.CanFastTask())
	testx.False(t, memory.FlowStageCategoryConfirmed.CanFastTask())
	testx.True(t, memory.FlowStageParamsConfirmed.CanFastTask())
	testx.True(t, memory.FlowStagePlanConfirmed.CanFastTask())
	testx.True(t, memory.FlowStageScriptConfirmed.CanFastTask())
}

// TestIntentMemoryStagePipelineAgent verifies stage/pipeline/agent advancement and session isolation.
func TestIntentMemoryStagePipelineAgent(t *testing.T) {
	m := memory.NewIntentMemory()

	// Empty initially
	testx.Equal(t, memory.FlowStageEmpty, m.GetStage("s1"))
	testx.Empty(t, m.GetPipeline("s1"))
	testx.Empty(t, m.GetAgent("s1"))

	// Advance
	m.SetPipeline("s1", "mix_video")
	m.SetAgent("s1", "gold_scriptwriter")
	m.SetStage("s1", memory.FlowStageScriptConfirmed)

	testx.Equal(t, memory.FlowStageScriptConfirmed, m.GetStage("s1"))
	testx.Equal(t, "mix_video", m.GetPipeline("s1"))
	testx.Equal(t, "gold_scriptwriter", m.GetAgent("s1"))

	// Different sessions are isolated
	testx.Equal(t, memory.FlowStageEmpty, m.GetStage("s2"))
	testx.Empty(t, m.GetPipeline("s2"))
	testx.Empty(t, m.GetAgent("s2"))
}

// TestIntentMemoryCheckpointAndFailedRun verifies checkpoint save/load/clear + failed task_run memory.
func TestIntentMemoryCheckpointAndFailedRun(t *testing.T) {
	m := memory.NewIntentMemory()

	// Failed task_run memory
	m.RecordFailedTaskRun("s1", 42)
	testx.Equal(t, int64(42), m.GetLastFailedTaskRun("s1"))
	testx.Equal(t, int64(0), m.GetLastFailedTaskRun("s-unknown"))

	// Checkpoint save/load/clear
	cp := &react.Checkpoint{
		Messages: []react.Message{{Role: "user", Content: "hi"}},
		Step:     1,
		Query:    "original",
	}
	m.SaveCheckpoint("s1", cp)
	got := m.LoadCheckpoint("s1")
	testx.NotNil(t, got)
	testx.Equal(t, 1, got.Step)
	testx.Len(t, got.Messages, 1)
	testx.Equal(t, "original", got.Query)

	// A session without a saved checkpoint → nil
	testx.Nil(t, m.LoadCheckpoint("s-unknown"))

	m.ClearCheckpoint("s1")
	testx.Nil(t, m.LoadCheckpoint("s1"))
}

// TestIntentMemorySnapshotAndMarshalJSON verifies the Snapshot and MarshalJSON serialization.
func TestIntentMemorySnapshotAndMarshalJSON(t *testing.T) {
	m := memory.NewIntentMemory()

	// The snapshot of a non-advanced session has empty fields
	snap := m.Snapshot("s1")
	testx.Empty(t, snap["stage"])
	testx.Empty(t, snap["pipeline"])
	testx.Empty(t, snap["agent"])

	m.SetPipeline("s1", "mix_video")
	m.SetAgent("s1", "gold_scriptwriter")
	m.SetStage("s1", memory.FlowStageScriptConfirmed)

	snap = m.Snapshot("s1")
	testx.Equal(t, "script_confirmed", snap["stage"])
	testx.Equal(t, "mix_video", snap["pipeline"])
	testx.Equal(t, "gold_scriptwriter", snap["agent"])

	// MarshalJSON: only sessions with a stage are included
	raw, err := json.Marshal(m)
	testx.NoError(t, err)
	var out map[string]struct {
		Stage    string `json:"stage"`
		Pipeline string `json:"pipeline,omitempty"`
	}
	testx.NoError(t, json.Unmarshal(raw, &out))
	testx.Contains(t, out, "s1")
	testx.Equal(t, "script_confirmed", out["s1"].Stage)
	testx.Equal(t, "mix_video", out["s1"].Pipeline)
	_, hasEmpty := out["s-unknown"]
	testx.False(t, hasEmpty, "sessions without a stage should not appear in the serialized result")
}

// TestIntentMemoryTopicEid verifies storing/reading the clicked hot-topic eid + topic title.
func TestIntentMemoryTopicEid(t *testing.T) {
	m := memory.NewIntentMemory()
	testx.Empty(t, m.GetTopicEid("s1"), "the eid is empty before any click")
	testx.Empty(t, m.GetTopicTitle("s1"), "the title is empty before any click")

	m.SetTopicEid("s1", "eid-abc123")
	m.SetTopicTitle("s1", "选题一")
	testx.Equal(t, "eid-abc123", m.GetTopicEid("s1"))
	testx.Equal(t, "选题一", m.GetTopicTitle("s1"))

	// The snapshot should expose the click information
	snap := m.Snapshot("s1")
	testx.Equal(t, "eid-abc123", snap["topicEid"])
	testx.Equal(t, "选题一", snap["topicTitle"])

	// Different sessions are isolated
	testx.Empty(t, m.GetTopicEid("s2"))
}
