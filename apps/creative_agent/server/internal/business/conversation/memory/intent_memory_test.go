package memory_test

import (
	"encoding/json"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestFlowStage_Valid 验证 FlowStage 状态常量与合法性判断。
func TestFlowStage_Valid(t *testing.T) {
	// 合法值
	for _, s := range []memory.FlowStage{
		memory.FlowStageEmpty,
		memory.FlowStageTopicConfirmed,
		memory.FlowStageCategoryConfirmed,
		memory.FlowStageParamsConfirmed,
		memory.FlowStagePlanConfirmed,
		memory.FlowStageScriptConfirmed,
	} {
		testx.True(t, s.Valid(), "%q 应为合法 FlowStage", s)
	}
	// 非法值
	testx.False(t, memory.FlowStage("bogus").Valid())
}

// TestFlowStage_CanFastTask 验证 CanFastTask 判断：
// params/plan/script 已确认 → 可触发快任务；早期阶段/未推进 → 否。
func TestFlowStage_CanFastTask(t *testing.T) {
	testx.False(t, memory.FlowStageEmpty.CanFastTask())
	testx.False(t, memory.FlowStageTopicConfirmed.CanFastTask())
	testx.False(t, memory.FlowStageCategoryConfirmed.CanFastTask())
	testx.True(t, memory.FlowStageParamsConfirmed.CanFastTask())
	testx.True(t, memory.FlowStagePlanConfirmed.CanFastTask())
	testx.True(t, memory.FlowStageScriptConfirmed.CanFastTask())
}

// TestIntentMemoryStagePipelineAgent 验证 stage/pipeline/agent 推进与 session 隔离。
func TestIntentMemoryStagePipelineAgent(t *testing.T) {
	m := memory.NewIntentMemory()

	// 初始为空
	testx.Equal(t, memory.FlowStageEmpty, m.GetStage("s1"))
	testx.Empty(t, m.GetPipeline("s1"))
	testx.Empty(t, m.GetAgent("s1"))

	// 推进
	m.SetPipeline("s1", "mix_video")
	m.SetAgent("s1", "gold_scriptwriter")
	m.SetStage("s1", memory.FlowStageScriptConfirmed)

	testx.Equal(t, memory.FlowStageScriptConfirmed, m.GetStage("s1"))
	testx.Equal(t, "mix_video", m.GetPipeline("s1"))
	testx.Equal(t, "gold_scriptwriter", m.GetAgent("s1"))

	// 不同 session 隔离
	testx.Equal(t, memory.FlowStageEmpty, m.GetStage("s2"))
	testx.Empty(t, m.GetPipeline("s2"))
	testx.Empty(t, m.GetAgent("s2"))
}

// TestIntentMemoryCheckpointAndFailedRun 验证 checkpoint 存/取/清 + 失败 task_run 记忆。
func TestIntentMemoryCheckpointAndFailedRun(t *testing.T) {
	m := memory.NewIntentMemory()

	// 失败 task_run 记忆
	m.RecordFailedTaskRun("s1", 42)
	testx.Equal(t, int64(42), m.GetLastFailedTaskRun("s1"))
	testx.Equal(t, int64(0), m.GetLastFailedTaskRun("s-unknown"))

	// checkpoint 存/取/清
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

	// 未保存的 session → nil
	testx.Nil(t, m.LoadCheckpoint("s-unknown"))

	m.ClearCheckpoint("s1")
	testx.Nil(t, m.LoadCheckpoint("s1"))
}

// TestIntentMemorySnapshotAndMarshalJSON 验证 Snapshot 快照与 MarshalJSON 序列化。
func TestIntentMemorySnapshotAndMarshalJSON(t *testing.T) {
	m := memory.NewIntentMemory()

	// 未推进的 session 快照为空字段
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

	// MarshalJSON：仅包含有 stage 的 session
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
	testx.False(t, hasEmpty, "无 stage 的 session 不应出现在序列化结果")
}

// TestIntentMemoryTopicEid 验证热点卡点选的热点 eid + 选题标题的存取。
func TestIntentMemoryTopicEid(t *testing.T) {
	m := memory.NewIntentMemory()
	testx.Empty(t, m.GetTopicEid("s1"), "未点选时 eid 为空")
	testx.Empty(t, m.GetTopicTitle("s1"), "未点选时标题为空")

	m.SetTopicEid("s1", "eid-abc123")
	m.SetTopicTitle("s1", "选题一")
	testx.Equal(t, "eid-abc123", m.GetTopicEid("s1"))
	testx.Equal(t, "选题一", m.GetTopicTitle("s1"))

	// Snapshot 应暴露点选信息
	snap := m.Snapshot("s1")
	testx.Equal(t, "eid-abc123", snap["topicEid"])
	testx.Equal(t, "选题一", snap["topicTitle"])

	// 不同 session 隔离
	testx.Empty(t, m.GetTopicEid("s2"))
}
