//go:build integration

// Package monitor stage-5 monitor job integration tests: against a real MySQL.
//
// How to run (requires MySQL):
//
//	cd base-server && go test -tags=integration ./internal/monitor/ -v
//
// Covers the stage-5 acceptance criteria (compensation retry + timeout fallback):
//   - status=0 compensation records are retried by RunCompensationJob: async_audit records resubmit (the auditing record is marked submitted)
//   - compensation retries are capped (3); abandoning beyond the cap refunds points + sets fail
//   - running projects older than 24h are refunded + set fail by RunTimeoutJob
package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	creationdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	riskmodel "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestCompensationJobRetriesAsyncAudit acceptance: a status=0 async_audit compensation record is retried → resubmitted.
//
// Setup: one status=auditing risk_audit_record + one matching compensation record (payload contains content_text).
// Run RunCompensationJob → RetryAsyncAudit reuses the original record's submitAuditRequest → task_input is written → compensation marked done.
func TestCompensationJobRetriesAsyncAudit(t *testing.T) {
	db := openMySQLTestDB(t)
	truncateMonitorTables(t, db)
	m := buildMonitorDeps(t, db, nil)
	ctx := context.Background()
	userID := int64(82001)
	sessionID := "session-monitor-async-audit"

	// Create the auditing record (the artifact before AsyncAudit failed; stored in cg_risk_audit_record after sharding)
	recordID, err := m.riskAudit.Insert(ctx, &riskmodel.AuditRecord{
		UserID: userID, ProjectID: sessionID,
		TaskType: "risk:async:dujiaapp_mixvideo60s_t2v_output", Status: "auditing",
	})
	testx.NoError(t, err)

	// Create the compensation record (status=0); the payload simulates what insertAuditCompensation writes
	payload, _ := json.Marshal(map[string]any{
		"biz_id":       "pl-99001",
		"sub_product":  riskmodel.MixVideo60sT2VOutputSubproduct,
		"third_id":     sessionID,
		"callback_url": "/inner/callback/risk",
		"user_id":      userID,
		"record_id":    recordID,
		"content_text": "一段成片视频描述",
	})
	_, err = m.comp.Insert(ctx, &creationdomain.Compensation{
		UserID: userID, ProjectID: sessionID,
		BizID: fmt.Sprintf("%d", recordID), BizType: creationdomain.BizTypeAsyncAudit,
		Payload: string(payload), Status: creationdomain.CompensationStatusPending,
	})
	testx.NoError(t, err)

	// Run the compensation job
	testx.NoError(t, RunCompensationJob(ctx, m.deps))

	// The compensation is marked done
	comp, err := m.comp.GetByBizID(ctx, fmt.Sprintf("%d", recordID), creationdomain.BizTypeAsyncAudit)
	testx.NoError(t, err)
	testx.Equal(t, creationdomain.CompensationStatusDone, comp.Status, "retry success → compensation marked done")

	// The auditing record is marked submitted (task_input holds sub_product + the callback bond)
	record, err := m.riskAudit.Get(ctx, recordID)
	testx.NoError(t, err)
	testx.Equal(t, "auditing", record.Status, "the record stays auditing (awaiting the risk callback)")
	testx.Contains(t, record.TaskInput, "sub_product", "task_input holds the submission marker after retry")
}

// TestCompensationJobAbandonsOverLimit acceptance: compensation retries beyond the cap (3) → abandon + refund points + set fail.
//
// Setup: a running project with points deducted + an async_audit compensation record with retry_count=2
// (payload missing content_text, so the retry must fail). Run the job → retry fails → retry_count reaches 3 →
// abandon → refund points + set fail.
func TestCompensationJobAbandonsOverLimit(t *testing.T) {
	db := openMySQLTestDB(t)
	truncateMonitorTables(t, db)
	m := buildMonitorDeps(t, db, nil)
	ctx := context.Background()
	userID := int64(82002)
	sessionID := "session-monitor-abandon"

	// Seed the balance + deduct points + create the running project (points already deducted)
	seedBalance(t, db, userID, 100)
	now := time.Now().Unix()
	testx.NoError(t, db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, ?, ?, 1, 10, 'consume', 1, ?)",
		userID, sessionID, sessionID+"_1", now,
	).Error)
	testx.NoError(t, m.projects.Insert(ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusRunning,
		PointsStatus: creationdomain.PointStatusUsed, PointsCost: 10,
		PointsOrderID: sessionID + "_1", RetryNum: 1,
	}))

	// Compensation record: retry_count=2, payload missing content_text (the retry must fail)
	payload, _ := json.Marshal(map[string]any{
		"biz_id":       "pl-99002",
		"sub_product":  riskmodel.MixVideo60sT2VOutputSubproduct,
		"third_id":     sessionID,
		"callback_url": "/inner/callback/risk",
		"user_id":      userID,
		"record_id":    0,
	})
	_, err := m.comp.Insert(ctx, &creationdomain.Compensation{
		UserID: userID, ProjectID: sessionID,
		BizID: "pl-99002", BizType: creationdomain.BizTypeAsyncAudit,
		Payload: string(payload), Status: creationdomain.CompensationStatusPending,
		RetryCount: 2,
	})
	testx.NoError(t, err)

	// Run the compensation job
	testx.NoError(t, RunCompensationJob(ctx, m.deps))

	// The compensation is marked done (abandoned)
	comp, err := m.comp.GetByBizID(ctx, "pl-99002", creationdomain.BizTypeAsyncAudit)
	testx.NoError(t, err)
	testx.Equal(t, creationdomain.CompensationStatusDone, comp.Status, "beyond the cap → abandoned")
	testx.Equal(t, 3, comp.RetryCount, "the retry count accumulates to 3")

	// Fallback: the project refunds points + sets fail
	project, err := m.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(t, err)
	testx.Equal(t, creationdomain.ProjectStatusFail, project.Status, "abandoning sets fail as the fallback")
	testx.Equal(t, creationdomain.PointStatusRefund, project.PointsStatus, "abandoning refunds points as the fallback")
	testx.Contains(t, project.FailReason, "超时", "the fallback goes through RefundAndFailTimeout")
}

// TestTimeoutJobRefundsStuckProjects acceptance: running projects older than 24h refund points + set fail.
func TestTimeoutJobRefundsStuckProjects(t *testing.T) {
	db := openMySQLTestDB(t)
	truncateMonitorTables(t, db)
	m := buildMonitorDeps(t, db, nil)
	ctx := context.Background()
	userID := int64(82003)
	sessionID := "session-monitor-timeout"

	// Seed the balance + deduct points + create a project created 25h ago, still running
	seedBalance(t, db, userID, 100)
	now := time.Now().Unix()
	testx.NoError(t, db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, ?, ?, 1, 10, 'consume', 1, ?)",
		userID, sessionID, sessionID+"_1", now,
	).Error)
	stuckTime := time.Now().Add(-25 * time.Hour).Unix()
	testx.NoError(t, m.projects.Insert(ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusRunning,
		PointsStatus: creationdomain.PointStatusUsed, PointsCost: 10,
		PointsOrderID: sessionID + "_1", RetryNum: 1,
		CreateTime: stuckTime, UpdateTime: stuckTime,
	}))

	// Run the timeout job
	testx.NoError(t, RunTimeoutJob(ctx, m.deps, 24))

	project, err := m.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(t, err)
	testx.Equal(t, creationdomain.ProjectStatusFail, project.Status, "the timed-out project is set fail")
	testx.Equal(t, creationdomain.PointStatusRefund, project.PointsStatus, "the timed-out project refunds points")

	// Balance: seed(100) + refund(10) = 110 (refunds are flow_type=2 credits, added back to the balance)
	balance, err := m.points.GetBalance(ctx, userID)
	testx.NoError(t, err)
	testx.Equal(t, 110, balance, "the timeout fallback refunds points (seed 100 + refund 10)")
}

// TestTimeoutJobSkipsFreshProjects acceptance: running projects within 24h are not processed by the timeout job.
func TestTimeoutJobSkipsFreshProjects(t *testing.T) {
	db := openMySQLTestDB(t)
	truncateMonitorTables(t, db)
	m := buildMonitorDeps(t, db, nil)
	ctx := context.Background()
	userID := int64(82004)
	sessionID := "session-monitor-fresh"

	testx.NoError(t, m.projects.Insert(ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusRunning,
		PointsStatus: creationdomain.PointStatusUnUsed, PointsCost: 10,
		RetryNum: 1, CreateTime: time.Now().Unix(),
	}))

	testx.NoError(t, RunTimeoutJob(ctx, m.deps, 24))

	project, err := m.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(t, err)
	testx.Equal(t, creationdomain.ProjectStatusRunning, project.Status, "projects within 24h are not timed out")
}
