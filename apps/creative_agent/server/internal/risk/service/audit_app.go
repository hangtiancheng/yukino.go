package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// RiskAuditApp is the risk-audit submission application service.
//
// It orchestrates the three submission modes:
//   - SyncAudit: synchronous submission (text/image), blocking for the result and returning immediately
//   - AsyncAudit: asynchronous submission (video), returning right after submit; the result closes via callback
//   - RetryAsyncAudit: the async compensation retry entry (called by the compensation job)
//
// All dependencies are interface-injected (domain.AuditBackend / domain.AuditRecordRepository /
// CompensationSink / domain.BlacklistChecker); it holds no gorm / redis.Client.
type RiskAuditApp struct {
	backend   domain.AuditBackend
	records   domain.AuditRecordRepository
	sink      CompensationSink
	blacklist domain.BlacklistChecker // when nil, blacklist checks are skipped and risk control proceeds as usual
}

// NewRiskAuditApp creates the audit-submission application service.
func NewRiskAuditApp(backend domain.AuditBackend, records domain.AuditRecordRepository, sink CompensationSink, blacklist domain.BlacklistChecker) *RiskAuditApp {
	return &RiskAuditApp{
		backend:   backend,
		records:   records,
		sink:      sink,
		blacklist: blacklist,
	}
}

// SyncAudit is synchronous text/image risk control: blocks for the result and returns immediately.
//
// Returns:
//   - passed=false + err=non-nil → the risk request failed (system error)
//   - passed=false + reason=non-empty → content rejected (risk hit)
//   - passed=true → passed
func (a *RiskAuditApp) SyncAudit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	// Image risk control: split into one image per call, rate-limited dispatch + one-vote veto (dispatch/aggregation policy converges in domain.SyncAuditImages)
	if len(req.Content.Image) > 0 {
		return domain.SyncAuditImages(ctx, a.backend, req, subProduct)
	}
	// Text risk control: a single request
	return a.doSyncRequest(ctx, req, subProduct)
}

// doSyncRequest actually calls the risk backend, returning (passed, reason, err).
func (a *RiskAuditApp) doSyncRequest(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	return a.backend.Submit(ctx, req, subProduct)
}

// AsyncAudit is asynchronous video risk control: returns immediately after submission; the result closes via /inner/callback/risk.
//
// Two loop-closing bonds:
//  1. CallbackURL: carries "where to notify me when the audit finishes" with the request
//  2. extra.callback_record_id: echoed on callback so the server finds which record to reconcile
//
// Flow: insert one status=auditing audit record first → submit the risk request (with callback params) →
// on submission failure write the compensation table (sink, handled by the Phase 3 compensation job) → return the recordID.
func (a *RiskAuditApp) AsyncAudit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string, callbackURL string) (int64, error) {
	// 1. Insert one status=auditing audit record first (task_type looks like "risk:async:{subProduct}")
	insertedID, err := a.records.Insert(ctx, &domain.AuditRecord{
		UserID:    req.UserID,
		ProjectID: req.ThirdID,
		TaskType:  "risk:async:" + subProduct,
		Status:    domain.StatusAuditing,
	})
	if err != nil {
		return 0, err
	}
	recordID := int64(insertedID)

	// 2. Submit the risk request with the callback loop-closing params
	req.CallbackURL = callbackURL
	if req.Extra == nil {
		req.Extra = map[string]string{}
	}
	req.Extra["callback_record_id"] = strconv.FormatInt(recordID, 10)

	if err := a.submitAuditRequest(ctx, req, subProduct); err != nil {
		// Submission failed → write the compensation table for the retry job (silently skipped when no sink is injected)
		if a.sink != nil {
			_ = a.sink.InsertAuditCompensation(ctx, req, subProduct, recordID)
		}
		return 0, err
	}

	return recordID, nil
}

// submitAuditRequest submits the async risk request to the backend (a returned err means submission failed).
//
// The locally reproduced backend decides synchronously; async submission semantics are simulated by "marking the audit record as submitted":
// the submission state is written back into the auditing record's task_input (visible for audit reconciliation) instead of really blocking.
// For real integration, replace with POST /async/audit (carrying CallbackURL).
func (a *RiskAuditApp) submitAuditRequest(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Validation: async requires content (video/text); an empty request counts as submission failure
	if len(req.Content.Text) == 0 && len(req.Content.Image) == 0 {
		return fmt.Errorf("async audit content is empty")
	}

	// Record this submission's inputs (subProduct + callback bonds) into the record for reconciliation
	submitted, _ := json.Marshal(map[string]any{
		"sub_product":        subProduct,
		"callback_url":       req.CallbackURL,
		"callback_record_id": req.Extra["callback_record_id"],
	})
	return a.records.Update(ctx, mustParseRecordID(req.Extra["callback_record_id"]), map[string]any{
		"task_input": string(submitted),
	})
}

// RetryAsyncAudit is the async risk retry entry called by the compensation job.
//
// payload is the JSON written by compensation, containing:
//   - biz_id / sub_product / third_id / callback_url / user_id / record_id / content_text
//
// Retry semantics: **reuse the original auditing record** through submitAuditRequest (rather than rerunning the full AsyncAudit —
// otherwise the still-empty content would write another compensation record, causing an infinite loop). An empty content_text counts as retry failure;
// the compensation job accumulates retry_count, and abandoning beyond the cap goes through the timeout-fallback points refund.
func (a *RiskAuditApp) RetryAsyncAudit(ctx context.Context, payload map[string]any) error {
	subProduct := asString(payload["sub_product"])
	if subProduct == "" {
		return fmt.Errorf("retry async audit: missing sub_product")
	}
	recordID := asInt64(payload["record_id"])
	if recordID <= 0 {
		return fmt.Errorf("retry async audit: missing record_id")
	}
	req := &domain.RiskSubmitRequest{
		UserID:      asInt64(payload["user_id"]),
		ThirdID:     asString(payload["third_id"]),
		BizID:       asString(payload["biz_id"]),
		CallbackURL: asString(payload["callback_url"]),
		Content:     domain.AuditContent{Text: asString(payload["content_text"])},
	}
	if req.Content.Text == "" {
		return fmt.Errorf("retry async audit: missing content")
	}
	req.Extra = map[string]string{"callback_record_id": strconv.FormatInt(recordID, 10)}
	return a.submitAuditRequest(ctx, req, subProduct)
}

// IsBlacklisted is the pre-submission blacklist check (passes when blacklist is not injected or subProduct is empty).
func (a *RiskAuditApp) IsBlacklisted(ctx context.Context, userID int64, subProduct string) bool {
	return a.checkRiskBlacklist(ctx, userID, subProduct)
}

// NewSyncAuditRecorder creates the merged recorder of one sync risk-control round (risk-side implementation).
//
// creation.application calls this method through RiskPort to obtain the recorder implementation (cross-context dependency inversion:
// the interface is defined in risk.domain, the implementation in risk.infrastructure; creation depends only on the interface).
// taskType looks like "risk:{role}:{type}:{stage}" (e.g. "risk:5:mix_video:precreate"),
// letting audit reconciliation search by (role,type).
func (a *RiskAuditApp) NewSyncAuditRecorder(ctx context.Context, userID int64, projectID string, taskStepID int64, taskType string) domain.SyncAuditRecorder {
	return dao.NewSyncAuditRecorder(ctx, userID, projectID, taskStepID, taskType, a.backend, a.records)
}

// checkRiskBlacklist is the pre-submission blacklist check: a hit returns true (intercept);
// it passes when blacklist is not injected (nil) or subProduct is empty.
func (a *RiskAuditApp) checkRiskBlacklist(ctx context.Context, userID int64, subProduct string) bool {
	if a.blacklist == nil || subProduct == "" {
		return false
	}
	return a.blacklist.IsBlacklisted(ctx, userID, subProduct)
}

// --- Helpers ---

// mustParseRecordID parses callback_record_id from extra; returns 0 on anomalies (a placeholder the normal flow never reaches).
func mustParseRecordID(s string) uint64 {
	id, _ := strconv.ParseUint(s, 10, 64)
	return id
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
