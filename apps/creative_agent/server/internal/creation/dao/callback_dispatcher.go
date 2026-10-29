package dao

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/service"
)

// CallbackHandler is the handler signature of task_detail callbacks (registered by task_type).
// Handlers must use records.MarkStatusIf themselves as the data-layer optimistic-lock idempotency gate —
// only running transitions; done/fail short-circuit directly, preventing concurrent duplicate callbacks.
type CallbackHandler func(ctx context.Context, d *CallbackDeps, req *contracts.CallbackReq) error

// CallbackDeps holds the cross-domain dependencies needed by callback handlers (repos + ports).
//
// The callback dispatcher lives in the infrastructure layer; handlers internally coordinate creation repositories with the risk/points
// cross-domain ports (service.RiskPort / service.PointsPort), without touching the risk/points
// infrastructure implementations directly — keeping dependencies one-way (creation → risk/points).
type CallbackDeps struct {
	projects domain.ProjectRepository
	records  domain.CallbackRecordRepository
	comp     domain.CompensationRepository
	risk     service.RiskPort
	points   service.PointsPort
}

// NewCallbackDeps creates the callback-handler dependency set (constructed and injected by the dispatcher at assembly time).
func NewCallbackDeps(projects domain.ProjectRepository, records domain.CallbackRecordRepository,
	comp domain.CompensationRepository, risk service.RiskPort, points service.PointsPort) *CallbackDeps {
	return &CallbackDeps{
		projects: projects,
		records:  records,
		comp:     comp,
		risk:     risk,
		points:   points,
	}
}

// CallbackDispatcher is the task-callback dispatcher: parse task_type → execute the handler asynchronously.
//
// Idempotency gate (aligned with spec §5.1): before a handler runs, CallbackRecordRepository.MarkStatusIf
// provides the data-layer optimistic lock — only running transitions (done/fail short-circuit), preventing concurrent duplicate callbacks.
//
// ⚠️ Core design (§4.2): HTTP returns 200 immediately; handlers run asynchronously in goroutines —
// preventing operators from timing out and retrying while waiting for business logic.
type CallbackDispatcher struct {
	handlers map[string]CallbackHandler
	deps     *CallbackDeps
}

// NewCallbackDispatcher creates the callback dispatcher, registering all task_type → handler mappings.
func NewCallbackDispatcher(deps *CallbackDeps) *CallbackDispatcher {
	d := &CallbackDispatcher{
		handlers: make(map[string]CallbackHandler),
		deps:     deps,
	}
	d.registerHandlers()
	return d
}

// registerHandlers registers the callback handlers (routed by task_type).
func (d *CallbackDispatcher) registerHandlers() {
	d.handlers[domain.TaskTypeKey(domain.RoleTypeScreenplay, domain.ProjectTypeMixVideo)] = HandleMixVideoScriptCallback
	d.handlers[domain.TaskTypeKey(domain.RoleTypeEditor, domain.ProjectTypeMixVideo)] = HandleMixVideoProjectCallback
}

// Dispatch dispatches one task callback (the /inner/callback/task_detail entry).
//
// Flow:
//  1. Parse extend_params to get task_type + callback_record_id
//  2. Update callback_record (record the raw callback output task_output, **without changing status** —
//     status transitions are each handler's responsibility (done/fail), avoiding overwriting a terminal state with success and breaking the short-circuit)
//  3. Look up the handler in the registry; return FailCallbackResp when missing
//  4. Execute the handler asynchronously in a goroutine (context.WithoutCancel); on failure write the compensation table
//     (BizTypeCallbackAgent, retried by the stage-5 compensation job)
func (d *CallbackDispatcher) Dispatch(ctx context.Context, req *contracts.CallbackReq) *contracts.CallbackResp {
	// 0. Parse task_type from extend_params
	passThrough, err := contracts.ParseExtendParams(req.ExtendParams)
	if err != nil {
		return contracts.FailCallbackResp("failed to parse extend_params: " + err.Error())
	}
	taskType := passThrough.TaskType

	// 1. Record the raw callback output (task_output + callback time), but **do not change status**
	if passThrough.CallbackRecordID > 0 {
		_ = d.deps.records.Update(ctx, uint64(passThrough.CallbackRecordID), map[string]any{
			"task_output": string(req.SpecialInfo),
		})
	}

	// 2. Find the handler
	handler, ok := d.handlers[taskType]
	if !ok {
		return contracts.FailCallbackResp("unknown task_type: " + taskType)
	}

	// 3. ⚠️ Key: HTTP returns 200 immediately; the handler runs asynchronously in a goroutine
	//    to prevent operators from retrying while waiting
	go func() {
		if err := handler(context.WithoutCancel(ctx), d.deps, req); err != nil {
			// Failure → write the compensation table (retried by the stage-5 compensation job)
			d.insertCallbackAgentCompensation(context.Background(), req, taskType)
		}
	}()

	return contracts.SuccessCallbackResp()
}

// RetryCallbackAgent is the callback-agent retry entry called by the compensation job.
//
// payload is the JSON written by insertCallbackAgentCompensation, containing:
//   - task_id / project_id / user_id / task_status / task_type
//
// Retry semantics: rerun the callback dispatch. Handlers have the data-layer optimistic-lock idempotency gate (MarkStatusIf:
// done/fail short-circuit directly), and Dispatch only writes the raw callback output without changing terminal states — repeated retries are naturally idempotent,
// never causing double refunds. A returned error means dispatch failed (unknown task_type / extend_params parse failure, etc.);
// the compensation job accumulates the retry count.
func (d *CallbackDispatcher) RetryCallbackAgent(ctx context.Context, payload map[string]any) error {
	req := &contracts.CallbackReq{
		TaskID:     asInt64(payload["task_id"]),
		ProjectID:  asString(payload["project_id"]),
		UserID:     asInt64(payload["user_id"]),
		TaskStatus: asString(payload["task_status"]),
	}
	if req.ProjectID == "" || req.TaskStatus == "" {
		return fmt.Errorf("retry callback agent: missing project_id/task_status")
	}
	// Compensation has no original extend_params; reconstruct it from the task_type stored in the payload
	taskType := asString(payload["task_type"])
	if taskType == "" {
		return fmt.Errorf("retry callback agent: missing task_type")
	}
	req.ExtendParams = contracts.BuildExtendParamsJSON(taskType, asInt64(payload["callback_record_id"]),
		domain.CallbackStepMulti, 0, 0)
	// Synchronous replay (no goroutine): dispatch failure returns err; handler success returns nil
	return d.redispatchCallback(ctx, req)
}

// redispatchCallback executes one callback dispatch synchronously (for compensation-job retries, no goroutine).
// A returned err means dispatch failed (unknown task_type, etc.); handler-internal errors are also returned as err.
func (d *CallbackDispatcher) redispatchCallback(ctx context.Context, req *contracts.CallbackReq) error {
	passThrough, err := contracts.ParseExtendParams(req.ExtendParams)
	if err != nil {
		return err
	}
	handler, ok := d.handlers[passThrough.TaskType]
	if !ok {
		return fmt.Errorf("unknown task_type: %s", passThrough.TaskType)
	}
	if passThrough.CallbackRecordID > 0 {
		_ = d.deps.records.Update(ctx, uint64(passThrough.CallbackRecordID), map[string]any{
			"task_output": string(req.SpecialInfo),
		})
	}
	return handler(context.WithoutCancel(ctx), d.deps, req)
}

// insertCallbackAgentCompensation writes the compensation table when a callback handler fails (BizTypeCallbackAgent).
// The payload stores everything needed for retry (including callback_record_id, so the compensation job can rebuild extend_params and replay).
func (d *CallbackDispatcher) insertCallbackAgentCompensation(ctx context.Context, req *contracts.CallbackReq, taskType string) {
	recordID := int64(0)
	if pt, err := contracts.ParseExtendParams(req.ExtendParams); err == nil {
		recordID = pt.CallbackRecordID
	}
	payload, _ := json.Marshal(map[string]any{
		"task_id":            req.TaskID,
		"project_id":         req.ProjectID,
		"user_id":            req.UserID,
		"task_status":        req.TaskStatus,
		"task_type":          taskType,
		"callback_record_id": recordID,
	})
	_, _ = d.deps.comp.Insert(ctx, &domain.Compensation{
		UserID:    req.UserID,
		ProjectID: req.ProjectID,
		BizID:     fmt.Sprintf("%d", req.TaskID),
		BizType:   domain.BizTypeCallbackAgent,
		Payload:   string(payload),
	})
}

// asString safely fetches a string from a map.
func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// asInt64 safely fetches an int64 from a map.
func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
