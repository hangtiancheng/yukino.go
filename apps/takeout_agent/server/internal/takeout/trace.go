package takeout

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// maxPayloadLength bounds a single trace payload; longer values are
// truncated to keep trace_json small.
const maxPayloadLength = 20000

// trace limits for the debug query APIs.
const (
	traceDefaultLimit = 200
	traceMaxLimit     = 1000
)

type traceScopeKey struct{}

// TraceEvent is one state-machine or agent-call event inside a trace.
type TraceEvent struct {
	StepOrder     int    `json:"stepOrder"`
	EventType     string `json:"eventType"`
	Phase         string `json:"phase"`
	AgentName     string `json:"agentName,omitempty"`
	ModelName     string `json:"modelName,omitempty"`
	InputPayload  string `json:"inputPayload,omitempty"`
	OutputPayload string `json:"outputPayload,omitempty"`
	LatencyMs     *int64 `json:"latencyMs,omitempty"`
	InputTokens   *int64 `json:"inputTokens,omitempty"`
	OutputTokens  *int64 `json:"outputTokens,omitempty"`
	TotalTokens   *int64 `json:"totalTokens,omitempty"`
	ErrorMessage  string `json:"errorMessage,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// TraceScope accumulates the events of one request. It is bound to a
// context by OpenTrace and flushed to the database by Close.
type TraceScope struct {
	store        *Store
	mu           sync.Mutex
	traceID      string
	sessionID    string
	userID       int64
	stepOrder    int
	startedAt    time.Time
	events       []TraceEvent
	status       string
	errorMessage string
	closed       bool
}

// TraceID returns the trace identifier of this scope.
func (s *TraceScope) TraceID() string { return s.traceID }

// addEvent appends one event under the scope lock.
func (s *TraceScope) addEvent(event TraceEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

// markFailed flags the whole trace as FAILED with an error summary.
func (s *TraceScope) markFailed(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = "FAILED"
	if s.errorMessage == "" {
		s.errorMessage = message
	}
}

// snapshot copies the mutable scope state for flushing.
func (s *TraceScope) snapshot() (status, errorMessage string, events []TraceEvent, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.errorMessage, append([]TraceEvent(nil), s.events...), len(s.events)
}

// TraceService records request traces and agent calls. Events are collected
// on the context-bound TraceScope and persisted when the scope closes,
// mirroring the Java ThreadLocal scope.
type TraceService struct {
	store *Store
}

// NewTraceService builds the trace service on top of the store.
func NewTraceService(store *Store) *TraceService {
	return &TraceService{store: store}
}

// OpenTrace starts a trace scope for one request and returns the context
// that carries it. The caller must call scope.Close when the request ends.
func (s *TraceService) OpenTrace(ctx context.Context, traceID, sessionID string, userID int64) (context.Context, *TraceScope) {
	scope := &TraceScope{
		store:     s.store,
		traceID:   traceID,
		sessionID: sessionID,
		userID:    userID,
		startedAt: time.Now(),
		status:    "SUCCESS",
	}
	return context.WithValue(ctx, traceScopeKey{}, scope), scope
}

// ScopeFromContext returns the trace scope bound to ctx, or nil.
func ScopeFromContext(ctx context.Context) *TraceScope {
	scope, _ := ctx.Value(traceScopeKey{}).(*TraceScope)
	return scope
}

// RecordEvent records one state-machine event on the current scope. Missing
// scopes are ignored so services stay usable outside a traced request.
func (s *TraceService) RecordEvent(ctx context.Context, eventType, phase string, input, output any) {
	s.record(ctx, eventType, phase, "", "", input, output, nil, nil, nil, nil, nil)
}

// RecordEventWithLatency records one state-machine event with a duration.
func (s *TraceService) RecordEventWithLatency(ctx context.Context, eventType, phase string, input, output any, latencyMs int64) {
	s.record(ctx, eventType, phase, "", "", input, output, &latencyMs, nil, nil, nil, nil)
}

// RecordError records a failure event and marks the trace FAILED.
func (s *TraceService) RecordError(ctx context.Context, eventType, phase string, input any, err error) {
	s.record(ctx, eventType, phase, "", "", input, nil, nil, nil, nil, nil, err)
}

// CallAgent runs one agent call and records an AGENT_CALL event with model
// name, latency, and token usage.
func (s *TraceService) CallAgent(ctx context.Context, sessionID, agentName string, agent *Agent, inputText string) (*AgentReply, error) {
	startedAt := time.Now()
	reply, err := agent.Call(ctx, inputText)
	latencyMs := time.Since(startedAt).Milliseconds()
	if err != nil {
		s.recordAgentCall(ctx, agentName, agent.ModelName, inputText, nil, latencyMs, err)
		return nil, err
	}
	s.recordAgentCall(ctx, agentName, agent.ModelName, inputText, reply, latencyMs, nil)
	return reply, nil
}

func (s *TraceService) recordAgentCall(ctx context.Context, agentName, modelName, inputText string, reply *AgentReply, latencyMs int64, err error) {
	var output any
	var inputTokens, outputTokens, totalTokens *int64
	if reply != nil {
		output = reply.Text
		if reply.HasUsage {
			in, out := reply.InputTokens, reply.OutputTokens
			inputTokens, outputTokens = &in, &out
			totalTokens = reply.TotalTokens()
		}
	}
	s.record(ctx, "AGENT_CALL", "AGENT", agentName, modelName, inputText, output, &latencyMs, inputTokens, outputTokens, totalTokens, err)
}

// record appends one TraceEvent to the scope bound to ctx.
func (s *TraceService) record(
	ctx context.Context,
	eventType, phase, agentName, modelName string,
	input, output any,
	latencyMs, inputTokens, outputTokens, totalTokens *int64,
	err error,
) {
	scope := ScopeFromContext(ctx)
	if scope == nil {
		return
	}
	var errorMessage string
	if err != nil {
		errorMessage = trimPayload(fmt.Sprintf("%T: %v", err, err))
	}
	scope.mu.Lock()
	scope.stepOrder++
	step := scope.stepOrder
	scope.mu.Unlock()
	scope.addEvent(TraceEvent{
		StepOrder:     step,
		EventType:     eventType,
		Phase:         phase,
		AgentName:     agentName,
		ModelName:     modelName,
		InputPayload:  toPayload(input),
		OutputPayload: toPayload(output),
		LatencyMs:     latencyMs,
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		TotalTokens:   totalTokens,
		ErrorMessage:  errorMessage,
		CreatedAt:     time.Now().Format(time.RFC3339Nano),
	})
	if errorMessage != "" {
		scope.markFailed(errorMessage)
	}
}

// Close flushes the accumulated events to takeout_request_trace exactly once.
func (s *TraceScope) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	if err := s.store.InsertTrace(s.toRow()); err != nil {
		slog.Warn("failed to persist request trace", "traceId", s.traceID, "error", err)
	}
}

// toRow converts the scope into a database row with the serialized
// trace_json document.
func (s *TraceScope) toRow() RequestTraceRow {
	status, errorMessage, events, count := s.snapshot()
	durationMs := time.Since(s.startedAt).Milliseconds()
	trace := map[string]any{
		"traceId":    s.traceID,
		"sessionId":  s.sessionID,
		"userId":     s.userID,
		"status":     status,
		"durationMs": durationMs,
		"events":     events,
	}
	row := RequestTraceRow{
		TraceID:    s.traceID,
		SessionID:  s.sessionID,
		UserID:     s.userID,
		Status:     status,
		EventCount: count,
		DurationMs: &durationMs,
		TraceJSON:  jsonString(trace),
	}
	if errorMessage != "" {
		row.ErrorMessage = &errorMessage
	}
	return row
}

// toPayload serializes a payload for storage: strings are trimmed directly,
// other values are JSON-encoded.
func toPayload(payload any) string {
	if payload == nil {
		return ""
	}
	if text, ok := payload.(string); ok {
		return trimPayload(text)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		// Degrade to fmt output but keep it visible: non-JSON payloads
		// cannot be parsed by the evaluation pipeline.
		slog.Warn("trace payload JSON marshal failed, falling back to fmt", "type", fmt.Sprintf("%T", payload), "error", err)
		return trimPayload(fmt.Sprintf("%v", payload))
	}
	return trimPayload(string(data))
}

// trimPayload truncates oversized payloads.
func trimPayload(text string) string {
	if len(text) <= maxPayloadLength {
		return text
	}
	return text[:maxPayloadLength] + "...[truncated]"
}

// FindByTraceID returns one trace row by trace ID.
func (s *TraceService) FindByTraceID(userID int64, traceID string) (*RequestTraceRow, error) {
	return s.store.FindTraceByID(userID, traceID)
}

// FindBySessionID returns the most recent traces of one session.
func (s *TraceService) FindBySessionID(userID int64, sessionID string, limit *int) ([]RequestTraceRow, error) {
	return s.store.FindTracesBySession(userID, sessionID, clampLimit(limit))
}

// FindByTimeRange returns traces inside [startAt, endAt).
func (s *TraceService) FindByTimeRange(userID int64, startAt, endAt time.Time, onlyUnlabeled bool, limit *int) ([]RequestTraceRow, error) {
	if startAt.IsZero() || endAt.IsZero() || !startAt.Before(endAt) {
		return nil, NewError("invalid trace query time range")
	}
	return s.store.FindTracesByTimeRange(userID, startAt, endAt, onlyUnlabeled, clampLimit(limit))
}

// UpdateLabel stores the human gold label of one trace.
func (s *TraceService) UpdateLabel(userID int64, traceID string, request TraceLabelRequest) error {
	if strings.TrimSpace(traceID) == "" {
		return NewError("traceId is required")
	}
	var expectedIntent, expectedClarifyAction *string
	if request.ExpectedIntent != "" {
		parsed, ok := ParseIntent(string(request.ExpectedIntent))
		if !ok {
			return NewError("invalid expectedIntent: %s", request.ExpectedIntent)
		}
		value := string(parsed)
		expectedIntent = &value
	}
	var expectedSlots *string
	if request.ExpectedSlots != nil {
		// Rebuild through NewSlotBundle so every dimension serializes as an
		// empty array instead of null, matching the Java SlotBundle
		// constructor normalization.
		normalized := NewSlotBundle(
			request.ExpectedSlots.MealTime,
			request.ExpectedSlots.Mood,
			request.ExpectedSlots.Scene,
			request.ExpectedSlots.HealthGoal,
			request.ExpectedSlots.Cuisine,
			request.ExpectedSlots.Taste,
			request.ExpectedSlots.Convenience,
		)
		value := jsonString(normalized)
		expectedSlots = &value
	}
	if request.ExpectedClarifyAction != "" {
		parsed, ok := ParseClarifyAction(string(request.ExpectedClarifyAction))
		if !ok {
			return NewError("invalid expectedClarifyAction: %s", request.ExpectedClarifyAction)
		}
		value := string(parsed)
		expectedClarifyAction = &value
	}
	updated, err := s.store.UpdateTraceLabel(userID, traceID, expectedIntent, expectedSlots, expectedClarifyAction, &userID, request.LabelNote)
	if err != nil {
		return err
	}
	if updated == 0 {
		return NewError("trace does not exist or you have no permission to label it")
	}
	return nil
}

func clampLimit(limit *int) int {
	if limit == nil {
		return traceDefaultLimit
	}
	value := *limit
	if value < 1 {
		return 1
	}
	if value > traceMaxLimit {
		return traceMaxLimit
	}
	return value
}
