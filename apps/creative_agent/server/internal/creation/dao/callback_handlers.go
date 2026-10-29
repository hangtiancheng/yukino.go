package dao

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	riskmodel "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// HandleMixVideoScriptCallback is the scriptwriter(role=3) script callback: segment text risk control → record=done + write the screenplay.
//
// Aligned with §4.3: the scriptwriter stage produces script segment text; synchronous risk control runs, and on pass record=done and the screenplay
// is written into ext.step_output["3:mix_video"] (the final-video pre_create fetches the screenplay from here as input).
// The scriptwriter has no points: a risk hit only sets record=fail, with no refund.
//
// Idempotency gate (aligned with spec §5.1): before processing, records.MarkStatusIf provides the data-layer optimistic lock —
// only running transitions; done/fail short-circuit directly, preventing concurrent duplicate callbacks.
func HandleMixVideoScriptCallback(ctx context.Context, d *CallbackDeps, req *contracts.CallbackReq) error {
	passThrough, err := contracts.ParseExtendParams(req.ExtendParams)
	if err != nil {
		return err
	}
	recordID := uint64(passThrough.CallbackRecordID)

	// Terminal short-circuit (preventing duplicate callbacks): return directly when the record is already done/fail.
	// ⚠️ Note: the record may not exist (the first pre_create wrote no record); then recordID=0
	// and no optimistic-lock check is done (the legacy implementation also short-circuits only when a record exists).
	if recordID > 0 {
		record, err := d.records.Get(ctx, recordID)
		if err == nil && (record.Status == domain.ProjectStatusDone || record.Status == domain.ProjectStatusFail) {
			return nil // already done; no duplicate processing
		}
	}

	switch req.TaskStatus {
	case domain.CallbackTaskStatusFailed:
		// Script generation failed → record=fail (the scriptwriter has no points; no refund)
		// ⚠️ Concurrency protection: updateRecordStatus returning false means a concurrent callback already transitioned it to terminal
		// (e.g. success→done); the successful result must not be overwritten — return with an idempotent short-circuit.
		if !updateRecordStatus(ctx, d.records, passThrough, domain.ProjectStatusFail) {
			return nil // already transitioned by another callback (concurrent re-entry / duplicate callback); idempotent short-circuit
		}
		return nil

	case domain.CallbackTaskStatusSuccess:
		// 1. Script segment text risk control (synchronous) — reusing the stage-3 SyncAuditRecorder
		scriptText := extractScriptText(req.SpecialInfo)
		riskReq := riskmodel.NewRiskSubmitRequest(req.UserID, req.ProjectID,
			fmt.Sprintf("script-%d", req.TaskID), scriptText)
		recorder := d.risk.NewSyncAuditRecorder(ctx, req.UserID, req.ProjectID, req.TaskID,
			"risk:mix_video:script")
		passed, reason, err := recorder.Run(riskReq, riskmodel.MixVideoT2IOutputSubproduct)
		recorder.Finish(passed, reason, err)

		if err != nil || !passed {
			// Hit → record=fail + output-risk card (no refund; the scriptwriter has no points)
			// ⚠️ Concurrency protection: same as the failed path — respect the MarkStatusIf CAS result;
			// do not overwrite when a concurrent transition already reached terminal.
			if !updateRecordStatus(ctx, d.records, passThrough, domain.ProjectStatusFail) {
				return nil // already transitioned by another callback (concurrent re-entry / duplicate callback); idempotent short-circuit
			}
			return nil
		}

		// 2. Pass → record=done + write the screenplay into ext.step_output["3:mix_video"].
		//    Data-layer optimistic lock: only the running → done atomic transition; done/fail short-circuit directly (preventing concurrent duplicate callbacks).
		if recordID > 0 {
			if done, _ := d.records.MarkStatusIf(ctx, recordID, domain.ProjectStatusDone, domain.ProjectStatusRunning); !done {
				return nil // already transitioned by another callback (concurrent re-entry / duplicate callback); idempotent short-circuit
			}
		}
		saveScreenplayToExt(ctx, d.projects, req, scriptText)
		return nil
	}
	return nil
}

// HandleMixVideoProjectCallback is the final-video(role=5) callback: the core of the audit intermediate state.
//
// Aligned with §4.4: after a successful final-video output it **must not go directly to done** — set audit first; the terminal state
// is decided only after the async video risk callback (decision 1).
//
// Flow: processing updates progress → failed refunds points + fail → success sets audit first (optimistic lock
// running→audit) → output text risk control → async video risk control (AsyncAudit) → recordID passed through.
//
// Idempotency gate (aligned with spec §5.1): the fetched project short-circuits on terminal states + the audit setting uses an optimistic lock,
// preventing concurrent duplicate callbacks.
func HandleMixVideoProjectCallback(ctx context.Context, d *CallbackDeps, req *contracts.CallbackReq) error {
	// Look up the work (project_id association)
	project, err := d.projects.GetByProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}

	// ⚠️ Terminal short-circuit: prevents duplicate operator callbacks (decision 1: terminal states are irreversible)
	if project.Status == domain.ProjectStatusDone || project.Status == domain.ProjectStatusFail {
		return nil
	}

	switch req.TaskStatus {
	case domain.CallbackTaskStatusProcessing:
		// Progress update: stay running, update progress
		project.Progress = req.ProgressPercentage
		_ = d.projects.Update(ctx, project)
		return nil

	case domain.CallbackTaskStatusFailed:
		// Direct failure: refund points + status=fail
		return failProjectCallback(ctx, d, project, contracts.BuildFailReason("任务失败", req.FailReason))

	case domain.CallbackTaskStatusSuccess:
		// ⚠️ Core logic: set audit first, then submit risk control (not done!)
		//    Users see "under review"; the risk callback decides the terminal state.
		//
		//    Atomicity (preventing the audit→done lost-update race): MarkAudit and the
		//    saveAsyncAuditRecordID pass-through were originally two independent full-field Updates; a risk callback
		//    arriving between the two writes would read audit → settle done, and then this handler's stale
		//    second Update would overwrite done back to audit (stuck forever). Fix: submit the async risk control first
		//    to obtain the recordID, then write status=audit + ext.async_audit_record_id in one CAS (UpdateIfStatus WHERE status='running'),
		//    eliminating the intermediate window; a CAS failure means another callback already advanced the state
		//    (e.g. the risk callback already settled) — idempotent short-circuit, no overwrite.
		if project.Status != domain.ProjectStatusRunning {
			return nil // not running (concurrent re-entry / already transitioned); idempotent short-circuit
		}

		// 1. Output title/body text risk control (synchronous)
		passed, reason, err := syncAuditOutputText(ctx, d, project, req.SpecialInfo)
		if err != nil || !passed {
			// Hit → refund points + fail + output-risk card
			return failProjectCallback(ctx, d, project, contracts.BuildFailReason("产出风控拦截", reason))
		}

		// 2. Output video risk control (async) — video review is slow; submit and return, closing the loop via callback
		videoReq := riskmodel.NewRiskSubmitRequest(project.UserID, project.ProjectID,
			fmt.Sprintf("%d", req.TaskID), "")
		videoReq.Content.Text = extractVideoDescription(req.SpecialInfo)

		recordID, err := d.risk.AsyncAudit(ctx, videoReq, riskmodel.MixVideo60sT2VOutputSubproduct, buildRiskCallbackURL())
		if err != nil {
			// Async submission failed → return the error; Dispatch writes the compensation table (stage 5 retries)
			return err
		}

		// 3. One CAS write of status=audit + ext.async_audit_record_id (running → audit)
		//    succeeds only while the DB status is still running; otherwise the risk callback already settled terminal — short-circuit.
		project.MarkAudit()
		setAsyncAuditRecordID(project, recordID)
		if ok, err := d.projects.UpdateIfStatus(ctx, project, domain.ProjectStatusRunning); err != nil {
			return err
		} else if !ok {
			return nil // already transitioned by another callback (the risk callback settled); idempotent short-circuit, no terminal overwrite
		}
		return nil
	}
	return nil
}

// updateRecordStatus updates the callback record status (data-layer optimistic lock: only running transitions); returns whether the transition succeeded.
//
// Aligned with spec §5.1: MarkStatusIf is the optimistic-lock idempotency gate — only running transitions;
// done/fail short-circuit directly, preventing concurrent duplicate callbacks. Returning false means another callback already transitioned it to terminal
// (e.g. concurrent success→done); callers must not overwrite terminal states and should short-circuit idempotently.
// With recordID<=0 there is no record to update (same semantics as the legacy implementation); returns false.
func updateRecordStatus(ctx context.Context, records domain.CallbackRecordRepository,
	passThrough *contracts.ExtendParams, status string) bool {
	if passThrough.CallbackRecordID <= 0 {
		return false
	}
	ok, _ := records.MarkStatusIf(ctx, uint64(passThrough.CallbackRecordID), status, domain.ProjectStatusRunning)
	return ok
}

// syncAuditOutputText runs output text risk control (synchronous), reusing the stage-3 SyncAudit port.
func syncAuditOutputText(ctx context.Context, d *CallbackDeps, project *domain.Project, specialInfo json.RawMessage) (bool, string, error) {
	text := extractScriptText(specialInfo)
	if text == "" {
		// No output text (possibly video only) → pass
		return true, "", nil
	}
	req := riskmodel.NewRiskSubmitRequest(project.UserID, project.ProjectID, "output_text", text)
	return d.risk.SyncAudit(ctx, req, riskmodel.MixVideoTitleT2TOutputSubproduct)
}

// failProjectCallback is the callback failure path: refund points + set fail + persist.
//
// Idempotent: the project terminal short-circuit is done by callers; MarkFail internally handles the
// "refund only when points_status=used" invariant (points.Refund is itself idempotent, returning nil when already refunded). Returning nil means handled.
func failProjectCallback(ctx context.Context, d *CallbackDeps, project *domain.Project, reason string) error {
	changed, err := project.MarkFail(reason, func() error {
		return d.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
	})
	if err != nil || !changed {
		return err
	}
	return d.projects.Update(ctx, project)
}

// saveScreenplayToExt writes the screenplay into ext.step_output["3:mix_video"] (the final-video stage fetches its input from here).
func saveScreenplayToExt(ctx context.Context, projects domain.ProjectRepository, req *contracts.CallbackReq, scriptText string) {
	project, err := projects.GetByProjectID(ctx, req.ProjectID)
	if err != nil {
		return
	}
	ext, _ := domain.ParseExt(project.Ext)
	ext.SetStepOutput(domain.RoleTypeScreenplay, domain.ProjectTypeMixVideo, scriptText)
	extJSON, _ := ext.ToJSON()
	project.Ext = extJSON
	_ = projects.Update(ctx, project)
}

// setAsyncAuditRecordID writes the async-risk recordID into the in-memory ext of project (no persistence).
//
// ⚠️ Unlike the old saveAsyncAuditRecordID: this only mutates memory; persistence happens in the caller's single CAS
// (UpdateIfStatus WHERE status='running') writing status=audit + ext,
// avoiding the lost-update race between two independent Updates for "set audit" and "pass through recordID".
func setAsyncAuditRecordID(project *domain.Project, recordID int64) {
	ext, _ := domain.ParseExt(project.Ext)
	if ext.AsyncAuditRecordID == 0 {
		ext.AsyncAuditRecordID = recordID
		extJSON, _ := ext.ToJSON()
		project.Ext = extJSON
	}
}

// extractScriptText extracts the screenplay text from the callback special_info.
// Local reproduction: special_info is JSON (or base64(JSON)); take the script/text field.
func extractScriptText(specialInfo json.RawMessage) string {
	raw := string(specialInfo)
	if raw == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err == nil {
		if s, ok := payload["script"].(string); ok {
			return s
		}
		if s, ok := payload["text"].(string); ok {
			return s
		}
	}
	return raw // fallback: the raw text becomes the audit text
}

// extractVideoDescription extracts the video description from special_info (for async risk review).
func extractVideoDescription(specialInfo json.RawMessage) string {
	raw := string(specialInfo)
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err == nil {
		if s, ok := payload["description"].(string); ok {
			return s
		}
	}
	return ""
}

// buildRiskCallbackURL builds the risk callback address (carried with the async risk submission request).
func buildRiskCallbackURL() string {
	return "/inner/callback/risk"
}
