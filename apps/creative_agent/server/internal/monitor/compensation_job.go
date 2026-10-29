package monitor

import (
	"context"
	"encoding/json"
	"fmt"

	creationdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// MaxCompensationRetry is the compensation retry cap: beyond it the record is abandoned (a points-refund fallback may be needed, see §5.3).
const MaxCompensationRetry = 3

// RunCompensationJob is the compensation-retry job: it scans status=0 compensation records and retries them by BizType.
//
// Retry semantics (aligned with §5.3):
//   - Success → MarkDone (status=1)
//   - Failure → IncrRetryCount; if RetryCount >= MaxCompensationRetry → abandon (MarkDone)
//
// A returned error only means the scan itself failed (DB query error); a single record's retry failure does not roll back the whole job.
func RunCompensationJob(ctx context.Context, deps Deps) error {
	if deps.Compensations == nil {
		return nil // no compensation repository injected (default assembly scenario); skip
	}
	pending, err := deps.Compensations.GetPending(ctx, 100)
	if err != nil {
		return err
	}
	for _, comp := range pending {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		retryErr := retryCompensation(ctx, deps, comp.BizType, comp.Payload)
		if retryErr == nil {
			_ = deps.Compensations.MarkDone(ctx, comp.ID)
			continue
		}
		// Retry failed: count +1; abandon beyond the cap
		_ = deps.Compensations.IncrRetryCount(ctx, comp.ID)
		if comp.RetryCount+1 >= MaxCompensationRetry {
			// Fallback before abandoning: async_audit records refund points + set fail (money never hangs)
			abandonCompensation(ctx, deps, comp.BizType, comp.Payload)
			_ = deps.Compensations.MarkDone(ctx, comp.ID)
		}
	}
	return nil
}

// retryCompensation dispatches the retry by BizType.
//
//   - BizTypeAsyncAudit(1) → riskAuditApp.RetryAsyncAudit (reusing the original auditing record to resubmit)
//   - BizTypeCallbackAgent(2) → dispatcher.RetryCallbackAgent (rebuilding extend_params and replaying synchronously)
func retryCompensation(ctx context.Context, deps Deps, bizType int, payload string) error {
	var p map[string]any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return fmt.Errorf("parse compensation payload: %w", err)
	}
	switch bizType {
	case creationdomain.BizTypeAsyncAudit:
		if deps.RiskAudit == nil {
			return fmt.Errorf("risk audit app not injected")
		}
		return deps.RiskAudit.RetryAsyncAudit(ctx, p)
	case creationdomain.BizTypeCallbackAgent:
		if deps.Dispatcher == nil {
			return fmt.Errorf("callback dispatcher not injected")
		}
		return deps.Dispatcher.RetryCallbackAgent(ctx, p)
	default:
		return fmt.Errorf("unknown compensation type: %d", bizType)
	}
}

// abandonCompensation is the fallback when a compensation retry is abandoned: async_audit records refund points + set fail.
//
// Aligned with §5.3 "fallback when compensation is abandoned": when async risk submission fails completely, the work may stay
// in running/audit forever, holding points. Look up the work by the payload's third_id; if still processable, refund points + set fail,
// guaranteeing the user's money comes back.
func abandonCompensation(ctx context.Context, deps Deps, bizType int, payload string) {
	if bizType != creationdomain.BizTypeAsyncAudit {
		return // callback_agent is only a card-push failure; money is unaffected — the timeout job covers it
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return
	}
	thirdID, _ := p["third_id"].(string)
	if thirdID == "" {
		return
	}
	project, err := deps.Projects.GetByProjectID(ctx, thirdID)
	if err != nil {
		return
	}
	if deps.PostCreate == nil {
		return
	}
	_ = deps.PostCreate.RefundAndFailTimeout(ctx, project)
}
