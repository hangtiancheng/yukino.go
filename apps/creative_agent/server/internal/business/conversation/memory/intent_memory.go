package memory

import (
	"encoding/json"
	"sync"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// FlowStage 是对话会话的意图推进状态（多 agent 串联的状态依据）。
//
// 对应真实系统的 intent_memory.FlowStage（05-subagent.md §5.5 路径 A）：
// 多 agent 链靠"用户确认卡片 → 下一次请求 → 快任务重新投递"闭环推进，
// 每一步走到的阶段记录在 session 级记忆里，作为下次请求判断的依据。
//
// 阶段推进顺序：
//
//	topic_confirmed → category_confirmed → params_confirmed → script_confirmed
//
// 关键语义（Step 3.6）：
//   - 不是每个 agent 都会推交互卡，只有需要用户决策的环节才停下等确认
//   - 下一次请求进来时先查 FlowStage：命中 script_confirmed 等就跳过 LLM，
//     直接用 GetNextAgent 算出下一个 agent 并投递新 task_run
type FlowStage string

// FlowStage 状态常量。
const (
	// FlowStageEmpty 初始状态（尚未进入多 agent 链）。
	FlowStageEmpty FlowStage = ""
	// FlowStageTopicConfirmed 选题已确认（用户点了选题卡）。
	FlowStageTopicConfirmed FlowStage = "topic_confirmed"
	// FlowStageCategoryConfirmed 品类已确认（用户点了品类卡）。
	FlowStageCategoryConfirmed FlowStage = "category_confirmed"
	// FlowStageParamsConfirmed 参数已确认（用户点了参数卡）。
	FlowStageParamsConfirmed FlowStage = "params_confirmed"
	// FlowStagePlanConfirmed 策划方案已确认。
	FlowStagePlanConfirmed FlowStage = "plan_confirmed"
	// FlowStageScriptConfirmed 脚本已确认（用户点了脚本卡）——此时应投递下一个 agent。
	FlowStageScriptConfirmed FlowStage = "script_confirmed"
)

// Valid 校验是否为合法的 FlowStage 值。
func (f FlowStage) Valid() bool {
	switch f {
	case FlowStageEmpty, FlowStageTopicConfirmed, FlowStageCategoryConfirmed,
		FlowStageParamsConfirmed, FlowStagePlanConfirmed, FlowStageScriptConfirmed:
		return true
	}
	return false
}

// CanFastTask 判断当前 FlowStage 是否能触发快任务（跳过 LLM 直接投递子任务）。
// 命中 params_confirmed / plan_confirmed / script_confirmed 时，
// 说明用户已完成上一张卡的确认，可以自动进入下一个 agent。
func (f FlowStage) CanFastTask() bool {
	switch f {
	case FlowStageParamsConfirmed, FlowStagePlanConfirmed, FlowStageScriptConfirmed:
		return true
	}
	return false
}

// IntentMemory 是 session 级意图记忆（存 FlowStage 推进状态）。
//
// 真实系统用 Redis/DB 持久化 session 记忆；复刻版用内存 map（session_id → 状态），
// 单进程即可满足对话链路验证。多实例部署时需换 Redis 实现。
type IntentMemory struct {
	mu       sync.RWMutex
	stages   map[string]FlowStage // session_id → FlowStage
	pipeline map[string]string    // session_id → 当前 pipeline（如 "mix_video"）
	agent    map[string]string    // session_id → 当前 agent（如 "gold_scriptwriter"）

	// Phase 4: Retry 快捷路径 —— session → 上次失败的 task_run_id
	lastFailedTaskRun map[string]int64
	// Phase 4: HIL 断点 —— session → checkpoint（中断时的恢复点）
	checkpoints map[string]*react.Checkpoint

	// Phase 5: 热点卡 —— session → 用户点选的热点 eid / 选题标题
	topicEid   map[string]string
	topicTitle map[string]string
}

// NewIntentMemory 创建空意图记忆。
func NewIntentMemory() *IntentMemory {
	return &IntentMemory{
		stages:            map[string]FlowStage{},
		pipeline:          map[string]string{},
		agent:             map[string]string{},
		lastFailedTaskRun: map[string]int64{},
		checkpoints:       map[string]*react.Checkpoint{},
		topicEid:          map[string]string{},
		topicTitle:        map[string]string{},
	}
}

// GetStage 读取 session 当前的 FlowStage。
func (m *IntentMemory) GetStage(sessionID string) FlowStage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stages[sessionID]
}

// SetStage 推进 session 的 FlowStage。
func (m *IntentMemory) SetStage(sessionID string, stage FlowStage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stages[sessionID] = stage
}

// GetPipeline 读取 session 当前所在 pipeline。
func (m *IntentMemory) GetPipeline(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pipeline[sessionID]
}

// SetPipeline 记录 session 当前所在 pipeline。
func (m *IntentMemory) SetPipeline(sessionID, pipeline string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pipeline[sessionID] = pipeline
}

// GetAgent 读取 session 当前已执行的 agent。
func (m *IntentMemory) GetAgent(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agent[sessionID]
}

// SetAgent 记录 session 当前已执行的 agent。
func (m *IntentMemory) SetAgent(sessionID, agent string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agent[sessionID] = agent
}

// RecordFailedTaskRun 记录 session 上次失败的 task_run（Retry 快捷路径复用其 plan）。
func (m *IntentMemory) RecordFailedTaskRun(sessionID string, taskRunID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastFailedTaskRun[sessionID] = taskRunID
}

// GetLastFailedTaskRun 读取 session 上次失败的 task_run_id（无则 0）。
func (m *IntentMemory) GetLastFailedTaskRun(sessionID string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastFailedTaskRun[sessionID]
}

// SaveCheckpoint 保存 session 的 HIL 断点。
func (m *IntentMemory) SaveCheckpoint(sessionID string, cp *react.Checkpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkpoints[sessionID] = cp
}

// LoadCheckpoint 读取 session 的 HIL 断点（无则 nil）。
func (m *IntentMemory) LoadCheckpoint(sessionID string) *react.Checkpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.checkpoints[sessionID]
}

// ClearCheckpoint 清除 session 的 HIL 断点（resume 完成后调用）。
func (m *IntentMemory) ClearCheckpoint(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.checkpoints, sessionID)
}

// SetTopicEid 记录 session 用户点选的热点 eid（热点卡确认后）。
func (m *IntentMemory) SetTopicEid(sessionID, eid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topicEid[sessionID] = eid
}

// GetTopicEid 读取 session 点选的热点 eid（无则空串）。
func (m *IntentMemory) GetTopicEid(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.topicEid[sessionID]
}

// SetTopicTitle 记录 session 用户点选的选题标题（热点卡确认后）。
func (m *IntentMemory) SetTopicTitle(sessionID, title string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topicTitle[sessionID] = title
}

// GetTopicTitle 读取 session 点选的选题标题（无则空串）。
func (m *IntentMemory) GetTopicTitle(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.topicTitle[sessionID]
}

// Snapshot 返回 session 的意图记忆快照（JSON 序列化用）。
func (m *IntentMemory) Snapshot(sessionID string) map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return map[string]string{
		"stage":      string(m.stages[sessionID]),
		"pipeline":   m.pipeline[sessionID],
		"agent":      m.agent[sessionID],
		"topicEid":   m.topicEid[sessionID],
		"topicTitle": m.topicTitle[sessionID],
	}
}

// MarshalJSON 实现 json.Marshaler（方便日志/调试输出）。
func (m *IntentMemory) MarshalJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	type view struct {
		Stage    string `json:"stage"`
		Pipeline string `json:"pipeline,omitempty"`
	}
	out := map[string]view{}
	for sid, stage := range m.stages {
		out[sid] = view{Stage: string(stage), Pipeline: m.pipeline[sid]}
	}
	return json.Marshal(out)
}
