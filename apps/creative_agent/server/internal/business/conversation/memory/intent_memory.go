package memory

import (
	"encoding/json"
	"sync"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"
)

// FlowStage is the intent-advancement state of a conversation session (the state basis of multi-agent chaining).
//
// Corresponds to the reference system's intent_memory.FlowStage (05-subagent.md §5.5 path A):
// the multi-agent chain advances through the "user confirms a card → next request → fast-task redispatch" loop;
// the stage reached at each step is recorded in session-level memory as the basis for the next request's decision.
//
// Stage advancement order:
//
//	topic_confirmed → category_confirmed → params_confirmed → script_confirmed
//
// Key semantics (Step 3.6):
//   - Not every agent pushes an interactive card; only steps needing a user decision stop and wait for confirmation
//   - The next request checks FlowStage first: hitting script_confirmed etc. skips the LLM and
//     directly computes the next agent with GetNextAgent, dispatching a new task_run
type FlowStage string

// FlowStage state constants.
const (
	// FlowStageEmpty is the initial state (not yet in a multi-agent chain).
	FlowStageEmpty FlowStage = ""
	// FlowStageTopicConfirmed: the topic is confirmed (the user clicked the topic card).
	FlowStageTopicConfirmed FlowStage = "topic_confirmed"
	// FlowStageCategoryConfirmed: the category is confirmed (the user clicked the category card).
	FlowStageCategoryConfirmed FlowStage = "category_confirmed"
	// FlowStageParamsConfirmed: the params are confirmed (the user clicked the params card).
	FlowStageParamsConfirmed FlowStage = "params_confirmed"
	// FlowStagePlanConfirmed: the plan is confirmed.
	FlowStagePlanConfirmed FlowStage = "plan_confirmed"
	// FlowStageScriptConfirmed: the script is confirmed (the user clicked the script card) — the next agent should now be dispatched.
	FlowStageScriptConfirmed FlowStage = "script_confirmed"
)

// Valid checks whether the value is a legal FlowStage.
func (f FlowStage) Valid() bool {
	switch f {
	case FlowStageEmpty, FlowStageTopicConfirmed, FlowStageCategoryConfirmed,
		FlowStageParamsConfirmed, FlowStagePlanConfirmed, FlowStageScriptConfirmed:
		return true
	}
	return false
}

// CanFastTask decides whether the current FlowStage can trigger a fast task (skipping the LLM and dispatching subtasks directly).
// Hitting params_confirmed / plan_confirmed / script_confirmed means
// the user finished confirming the previous card and the next agent can start automatically.
func (f FlowStage) CanFastTask() bool {
	switch f {
	case FlowStageParamsConfirmed, FlowStagePlanConfirmed, FlowStageScriptConfirmed:
		return true
	}
	return false
}

// IntentMemory is the session-level intent memory (storing FlowStage advancement state).
//
// The reference system persists session memory in Redis/DB; the replica uses an in-memory map (session_id → state),
// which suffices for single-process conversation-chain validation. Multi-instance deployments need a Redis implementation.
type IntentMemory struct {
	mu       sync.RWMutex
	stages   map[string]FlowStage // session_id → FlowStage
	pipeline map[string]string    // session_id → current pipeline (e.g. "mix_video")
	agent    map[string]string    // session_id → current agent (e.g. "gold_scriptwriter")

	// Phase 4: Retry fast path — session → last failed task_run_id
	lastFailedTaskRun map[string]int64
	// Phase 4: HIL breakpoint — session → checkpoint (the recovery point at interrupt)
	checkpoints map[string]*react.Checkpoint

	// Phase 5: hot-topic card — session → the hot-topic eid / topic title the user clicked
	topicEid   map[string]string
	topicTitle map[string]string
}

// NewIntentMemory creates an empty intent memory.
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

// GetStage reads the session's current FlowStage.
func (m *IntentMemory) GetStage(sessionID string) FlowStage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.stages[sessionID]
}

// SetStage advances the session's FlowStage.
func (m *IntentMemory) SetStage(sessionID string, stage FlowStage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stages[sessionID] = stage
}

// GetPipeline reads the session's current pipeline.
func (m *IntentMemory) GetPipeline(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pipeline[sessionID]
}

// SetPipeline records the session's current pipeline.
func (m *IntentMemory) SetPipeline(sessionID, pipeline string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pipeline[sessionID] = pipeline
}

// GetAgent reads the agent the session has currently executed.
func (m *IntentMemory) GetAgent(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agent[sessionID]
}

// SetAgent records the agent the session has currently executed.
func (m *IntentMemory) SetAgent(sessionID, agent string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agent[sessionID] = agent
}

// RecordFailedTaskRun records the session's last failed task_run (the Retry fast path reuses its plan).
func (m *IntentMemory) RecordFailedTaskRun(sessionID string, taskRunID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastFailedTaskRun[sessionID] = taskRunID
}

// GetLastFailedTaskRun reads the session's last failed task_run_id (0 when none).
func (m *IntentMemory) GetLastFailedTaskRun(sessionID string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastFailedTaskRun[sessionID]
}

// SaveCheckpoint saves the session's HIL breakpoint.
func (m *IntentMemory) SaveCheckpoint(sessionID string, cp *react.Checkpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkpoints[sessionID] = cp
}

// LoadCheckpoint reads the session's HIL breakpoint (nil when none).
func (m *IntentMemory) LoadCheckpoint(sessionID string) *react.Checkpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.checkpoints[sessionID]
}

// ClearCheckpoint clears the session's HIL breakpoint (called after resume completes).
func (m *IntentMemory) ClearCheckpoint(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.checkpoints, sessionID)
}

// SetTopicEid records the hot-topic eid the user clicked for the session (after hot-card confirmation).
func (m *IntentMemory) SetTopicEid(sessionID, eid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topicEid[sessionID] = eid
}

// GetTopicEid reads the clicked hot-topic eid of the session (empty string when none).
func (m *IntentMemory) GetTopicEid(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.topicEid[sessionID]
}

// SetTopicTitle records the topic title the user clicked for the session (after hot-card confirmation).
func (m *IntentMemory) SetTopicTitle(sessionID, title string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.topicTitle[sessionID] = title
}

// GetTopicTitle reads the clicked topic title of the session (empty string when none).
func (m *IntentMemory) GetTopicTitle(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.topicTitle[sessionID]
}

// Snapshot returns the session's intent-memory snapshot (for JSON serialization).
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

// MarshalJSON implements json.Marshaler (convenient for logging/debug output).
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
