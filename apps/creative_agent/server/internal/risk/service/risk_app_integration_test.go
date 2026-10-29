//go:build integration

// Package application_test risk application-service integration tests: against real MySQL + Redis.
//
// How to run (requires MySQL/Redis):
//
//	cd base-server && go test -tags=integration -p 1 ./internal/risk/... -count=1 -v
//
// Covers Task 6.2's submission + terminal-settling loop (risk application layer, through real gorm repositories):
//   - Sync submission SyncAudit: text passes / sensitive-word interception (the local backend's word list is set explicitly)
//   - Async submission AsyncAudit: inserts an auditing record + task_type=risk:async:{subProduct} + writes task_input on successful submission
//   - Async submission failure: writes a cg_audit_compensation record (biz_type=1); the compensation retry
//     RetryAsyncAudit reuses the original auditing record to resubmit
//   - Callback terminal settling AuditOutputApp: passed → audit_record=done + project=done (no refund);
//     !passed → audit_record=fail + project=fail + refund (idempotent); duplicate callbacks short-circuit on terminal, no double refund
//   - Sync merged recorder SyncAuditRecorder: multiple Runs + Finish merge into one record (real repository)
//
// Assembly (aligned with internal/app/app.go's New): the local sensitive-word backend (LocalAuditBackend,
// word list set explicitly) + real points/risk/creation repositories + the Redis blacklist checker.
// The governance loop (consumer/evaluation job/blacklist) is covered by internal/monitor/risk_governance_integration_test.go;
// this file does not repeat it.
package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	creationinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/dao"
	creationdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	pointsapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/application"
	pointsdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/domain"
	pointsinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/infrastructure"
	riskcontracts "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/contracts"
	riskinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	riskdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	riskapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx/suite"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// riskDeps holds the test-assembled new-architecture components (equivalent to internal/app/app.go's New output, focused on the risk side).
type riskDeps struct {
	db           *gorm.DB
	rdb          *redis.Client
	riskAudit    *riskapp.RiskAuditApp
	auditOutput  *riskapp.AuditOutputApp
	records      riskdomain.AuditRecordRepository // audit-record repository (for assertions)
	projects     creationdomain.ProjectRepository
	comp         creationdomain.CompensationRepository
	points       pointsapp.PointsApp
	auditBackend *riskinfra.LocalAuditBackend // tests can override the sensitive-word list
}

// RiskAppIntegrationSuite is the integration test suite.
type RiskAppIntegrationSuite struct {
	suite.Suite
	deps *riskDeps
}

func (s *RiskAppIntegrationSuite) SetupSuite() {
	t := s.T()
	db := openMySQLTestDB(t)
	rdb := openRedisTestDB(t)

	// AutoMigrate each bounded context's schema (idempotent; missing columns are added)
	testx.NoError(t, creationinfra.Migrate(db))
	testx.NoError(t, pointsinfra.Migrate(db))
	testx.NoError(t, riskinfra.Migrate(db))

	s.deps = buildRiskDeps(t, db, rdb)
}

func (s *RiskAppIntegrationSuite) SetupTest() {
	t := s.T()
	testx.NoError(t, s.deps.db.Exec("TRUNCATE TABLE cg_risk_audit_record").Error)
	testx.NoError(t, s.deps.db.Exec("TRUNCATE TABLE cg_risk_stats").Error)
	testx.NoError(t, s.deps.db.Exec("TRUNCATE TABLE cg_creation_project").Error)
	testx.NoError(t, s.deps.db.Exec("TRUNCATE TABLE cg_points_flow").Error)
	testx.NoError(t, s.deps.db.Exec("TRUNCATE TABLE cg_audit_compensation").Error)
	testx.NoError(t, s.deps.rdb.FlushDB(context.Background()).Err())
	// Reset the sensitive-word list per case (defaults contain "赌博" etc., preventing state leakage between cases)
	s.deps.auditBackend.SetSensitiveWords(riskinfra.DefaultSensitiveWords)
}

// TestSyncAuditPassAndBlock sync submission: clean text passes; sensitive-word hits are intercepted with a reason.
func (s *RiskAppIntegrationSuite) TestSyncAuditPassAndBlock() {
	ctx := context.Background()
	userID := int64(71001)
	s.deps.auditBackend.SetSensitiveWords([]string{"赌博", "代考"})

	// Clean text → passes
	passed, reason, err := s.deps.riskAudit.SyncAudit(ctx, riskdomain.NewRiskSubmitRequest(userID, "p-sync-pass", "b-sync-1", "今天天气不错"), "5:mix_video")
	testx.NoError(s.T(), err)
	testx.True(s.T(), passed)
	testx.Empty(s.T(), reason)

	// Sensitive-word text → intercepted; the reason contains the hit word
	passed, reason, err = s.deps.riskAudit.SyncAudit(ctx, riskdomain.NewRiskSubmitRequest(userID, "p-sync-block", "b-sync-2", "这里有个赌博网站"), "5:mix_video")
	testx.NoError(s.T(), err)
	testx.False(s.T(), passed)
	testx.Contains(s.T(), reason, "赌博")
}

// TestAsyncAuditWritesAuditingRecord async submission: inserts an auditing record + task_type=risk:async:{subProduct};
// a successful submission writes the submission inputs (sub_product + callback bonds) into task_input, and callback_url is backfilled into the request.
func (s *RiskAppIntegrationSuite) TestAsyncAuditWritesAuditingRecord() {
	ctx := context.Background()
	userID := int64(71002)
	subProduct := riskdomain.MixVideo60sT2VOutputSubproduct
	callbackURL := "http://cb/risk"
	req := &riskdomain.RiskSubmitRequest{
		UserID: userID, ThirdID: "p-async-ok", BizID: "b-async-1",
		Content: riskdomain.AuditContent{Text: "一段成片视频描述"},
	}

	recordID, err := s.deps.riskAudit.AsyncAudit(ctx, req, subProduct, callbackURL)
	testx.NoError(s.T(), err)
	testx.Greater(s.T(), recordID, int64(0))

	// There is exactly one auditing record in the DB; task_type contains the subProduct
	rec, err := s.deps.records.Get(ctx, uint64(recordID))
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), int64(userID), rec.UserID)
	testx.Equal(s.T(), "p-async-ok", rec.ProjectID)
	testx.Equal(s.T(), "risk:async:"+subProduct, rec.TaskType)
	testx.Equal(s.T(), riskdomain.StatusAuditing, rec.Status, "async submission inserts as auditing")

	// Callback bonds are backfilled into the request + task_input records the submission inputs
	testx.Equal(s.T(), callbackURL, req.CallbackURL)
	testx.Equal(s.T(), fmt.Sprintf("%d", recordID), req.Extra["callback_record_id"])
	testx.Contains(s.T(), rec.TaskInput, subProduct)
	testx.Contains(s.T(), rec.TaskInput, callbackURL)

	// No compensation record written (submission succeeded)
	_, err = s.deps.comp.GetByBizID(ctx, fmt.Sprintf("%d", recordID), creationdomain.BizTypeAsyncAudit)
	testx.ErrorIs(s.T(), err, creationdomain.ErrCompensationNotFound, "a successful submission writes no compensation")
}

// TestAsyncAuditSubmitFailureWritesCompensation async submission failure: empty content → submitAuditRequest
// validation fails → one biz_type=1 compensation record is written (reusing the original recordID); the original record stays auditing for retry reuse.
func (s *RiskAppIntegrationSuite) TestAsyncAuditSubmitFailureWritesCompensation() {
	ctx := context.Background()
	userID := int64(71003)
	subProduct := riskdomain.MixVideo60sT2VOutputSubproduct

	// Empty content (no text, no image) → submission fails
	req := &riskdomain.RiskSubmitRequest{UserID: userID, ThirdID: "p-async-fail", BizID: "b-async-2"}
	retRecordID, err := s.deps.riskAudit.AsyncAudit(ctx, req, subProduct, "")
	testx.Error(s.T(), err)
	testx.Equal(s.T(), int64(0), retRecordID, "a failed submission returns no recordID")

	// One biz_type=1 record is written to the compensation table; biz_id is the original recordID (the reconciliation bond)
	recordID := mustAtoiID(s.T(), req.Extra["callback_record_id"])
	comp, err := s.deps.comp.GetByBizID(ctx, fmt.Sprintf("%d", recordID), creationdomain.BizTypeAsyncAudit)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.BizTypeAsyncAudit, comp.BizType)
	testx.Equal(s.T(), creationdomain.CompensationStatusPending, comp.Status)
	testx.Equal(s.T(), userID, comp.UserID)
	// The payload contains the fields needed for retry (parsed by RetryAsyncAudit)
	payload := map[string]any{}
	testx.NoError(s.T(), json.Unmarshal([]byte(comp.Payload), &payload))
	testx.Equal(s.T(), subProduct, payload["sub_product"])
	testx.Equal(s.T(), "p-async-fail", payload["third_id"])
	testx.Equal(s.T(), float64(userID), payload["user_id"])

	// The original record stays auditing (reused by the compensation job's retry)
	rec, err := s.deps.records.Get(ctx, uint64(recordID))
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), riskdomain.StatusAuditing, rec.Status, "a failed-submission record stays auditing for retry")
}

// TestRetryAsyncAuditDrivesFromCompensationPayload compensation retry: drives RetryAsyncAudit from the compensation payload,
// reusing the original auditing record to resubmit (task_input gets the submission marker); no second record is created.
func (s *RiskAppIntegrationSuite) TestRetryAsyncAuditDrivesFromCompensationPayload() {
	ctx := context.Background()
	userID := int64(71004)
	subProduct := riskdomain.MixVideo60sT2VOutputSubproduct

	// Pre-create an auditing record + the matching compensation record (payload with content_text), equivalent to what the compensation job scans
	recordID, err := s.deps.records.Insert(ctx, &riskdomain.AuditRecord{
		UserID: userID, ProjectID: "p-retry", TaskType: "risk:async:" + subProduct,
		Status: riskdomain.StatusAuditing,
	})
	testx.NoError(s.T(), err)
	payload, _ := json.Marshal(map[string]any{
		"biz_id":       "pl-retry-1",
		"sub_product":  subProduct,
		"third_id":     "p-retry",
		"callback_url": "/inner/callback/risk",
		"user_id":      userID,
		"record_id":    recordID,
		"content_text": "一段成片视频描述",
	})
	_, err = s.deps.comp.Insert(ctx, &creationdomain.Compensation{
		UserID: userID, ProjectID: "p-retry",
		BizID: fmt.Sprintf("%d", recordID), BizType: creationdomain.BizTypeAsyncAudit,
		Payload: string(payload), Status: creationdomain.CompensationStatusPending,
	})
	testx.NoError(s.T(), err)

	// Drive the retry from the compensation payload (equivalent to monitor.RunCompensationJob's retryCompensation)
	testx.NoError(s.T(), s.deps.riskAudit.RetryAsyncAudit(ctx, map[string]any{
		"sub_product":  subProduct,
		"record_id":    int64(recordID),
		"user_id":      int64(userID),
		"third_id":     "p-retry",
		"biz_id":       "pl-retry-1",
		"callback_url": "/inner/callback/risk",
		"content_text": "一段成片视频描述",
	}))

	// The original record is reused (only one): status stays auditing (awaiting the risk callback); task_input gets the submission marker
	rec, err := s.deps.records.Get(ctx, recordID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), riskdomain.StatusAuditing, rec.Status)
	testx.Contains(s.T(), rec.TaskInput, `"sub_product":"`+subProduct+`"`)
	testx.Contains(s.T(), rec.TaskInput, fmt.Sprintf(`"callback_record_id":"%d"`, recordID))
}

// TestSyncAuditRecorderRealDB sync merged recorder (real repository): multiple Runs + Finish merge into one record;
// task_input records all bizIDs + subProducts of this round; a sensitive-word hit makes the status fail.
func (s *RiskAppIntegrationSuite) TestSyncAuditRecorderRealDB() {
	ctx := context.Background()
	userID := int64(71005)
	projectID := "p-recorder"
	s.deps.auditBackend.SetSensitiveWords([]string{"赌博"})

	recorder := s.deps.riskAudit.NewSyncAuditRecorder(ctx, userID, projectID, 5001, "risk:5:mix_video:precreate")
	p1, r1, err1 := recorder.Run(riskdomain.NewRiskSubmitRequest(userID, projectID, "b-rec-1", "把剧本做成视频"), "dujiaapp_mixvideo_t2t_input")
	testx.NoError(s.T(), err1)
	testx.True(s.T(), p1)
	p2, r2, err2 := recorder.Run(riskdomain.NewRiskSubmitRequest(userID, projectID, "b-rec-2", "这里赌博不行"), "dujiaapp_mixvideo60s_t2v_output")
	testx.NoError(s.T(), err2)
	testx.False(s.T(), p2)
	testx.Contains(s.T(), r2, "赌博")
	recorder.Finish(p1 && p2, firstNonEmpty(r1, r2), firstNonNil(err1, err2))

	// There is only one record: status fail (any rejection), with task_type and bizID/subProduct merged
	var row struct {
		ID        uint64
		UserID    int64
		ProjectID string
		TaskType  string
		TaskInput string
		Status    string
	}
	testx.NoError(s.T(), s.deps.db.Raw(
		"SELECT id, user_id, project_id, task_type, task_input, status FROM cg_risk_audit_record").Scan(&row).Error)
	// Behavior assertion: multiple Runs merge into exactly one record (a valid auto-increment ID suffices; the concrete value is a DB implementation detail, not asserted)
	var recordCount int64
	testx.NoError(s.T(), s.deps.db.Raw("SELECT COUNT(*) FROM cg_risk_audit_record").Scan(&recordCount).Error)
	testx.Equal(s.T(), int64(1), recordCount, "multiple Runs + Finish merge into one record")
	testx.Greater(s.T(), row.ID, uint64(0), "the merged record has a valid auto-increment ID")
	testx.Equal(s.T(), int64(userID), row.UserID)
	testx.Equal(s.T(), projectID, row.ProjectID)
	testx.Equal(s.T(), "risk:5:mix_video:precreate", row.TaskType)
	testx.Equal(s.T(), riskdomain.StatusFail, row.Status)

	var in riskinfra.SyncAuditTaskInput
	testx.NoError(s.T(), json.Unmarshal([]byte(row.TaskInput), &in))
	testx.ElementsMatch(s.T(), []string{"b-rec-1", "b-rec-2"}, in.BizIDs)
	testx.ElementsMatch(s.T(), []string{"dujiaapp_mixvideo_t2t_input", "dujiaapp_mixvideo60s_t2v_output"}, in.SubTypes)
}

// TestAuditOutputPassedSettlesDone a passed callback settles: audit_record → done, project → done, no points refund.
func (s *RiskAppIntegrationSuite) TestAuditOutputPassedSettlesDone() {
	ctx := context.Background()
	userID := int64(71006)
	sessionID := "session-risk-pass"
	seedBalance(s.T(), s.deps.db, userID, 100)
	seedConsumedProject(s.T(), s.deps.db, s.deps.projects, ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusRunning,
		PointsStatus: creationdomain.PointStatusUsed, PointsCost: 10,
		PointsOrderID: sessionID + "_1", RetryNum: 1,
	})
	recordID := seedAuditingRecord(s.T(), s.deps.records, ctx, userID, sessionID)

	// Callback passed=true
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, &riskcontracts.RiskCallbackReq{
		BizID: "dujiaapp_mixvideo60s_t2v_output-1", ThirdID: sessionID, UserID: userID,
		Passed: true, Extra: map[string]string{"callback_record_id": fmt.Sprintf("%d", recordID)},
	}))

	// audit_record → done
	rec, err := s.deps.records.Get(ctx, recordID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), riskdomain.StatusDone, rec.Status)

	// project → done; points deducted, not refunded
	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusUsed, project.PointsStatus)
	testx.Equal(s.T(), 90, mustBalance(s.T(), s.deps.points, ctx, userID), "a pass refunds nothing (100-10)")
}

// TestAuditOutputNotPassedRefundsAndFails a rejected callback settles: audit_record → fail, project → fail + points refund.
func (s *RiskAppIntegrationSuite) TestAuditOutputNotPassedRefundsAndFails() {
	ctx := context.Background()
	userID := int64(71007)
	sessionID := "session-risk-fail"
	seedBalance(s.T(), s.deps.db, userID, 100)
	seedConsumedProject(s.T(), s.deps.db, s.deps.projects, ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusRunning,
		PointsStatus: creationdomain.PointStatusUsed, PointsCost: 10,
		PointsOrderID: sessionID + "_1", RetryNum: 1,
	})
	recordID := seedAuditingRecord(s.T(), s.deps.records, ctx, userID, sessionID)

	// Callback passed=false
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, &riskcontracts.RiskCallbackReq{
		BizID: "dujiaapp_mixvideo60s_t2v_output-1", ThirdID: sessionID, UserID: userID,
		Passed: false, Reason: "输出包含违规内容",
		Extra: map[string]string{"callback_record_id": fmt.Sprintf("%d", recordID)},
	}))

	// audit_record → fail
	rec, err := s.deps.records.Get(ctx, recordID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), riskdomain.StatusFail, rec.Status)

	// project → fail + refund (seed 100 - deduct 10 + refund 10 = 110)
	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus)
	testx.Equal(s.T(), 110, mustBalance(s.T(), s.deps.points, ctx, userID), "a rejection refunds points (the deduction entry is marked refunded; the refund is added back)")

	// There is exactly one refund entry (no double refund)
	flows, err := s.deps.points.ListByProject(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), 2, len(flows), "two entries: deduction + refund (the seed credit with project_id='seed' is not in this list)")
	var refundN int
	for _, f := range flows {
		if f.FlowType == pointsdomain.FlowTypeRefund {
			refundN++
		}
	}
	testx.Equal(s.T(), 1, refundN, "points refunded exactly once")
}

// TestAuditOutputDuplicateCallbackNoDoubleRefund duplicate callbacks: audit-record terminal short-circuit + project terminal short-circuit →
// no repeated settling, no repeated refunds.
func (s *RiskAppIntegrationSuite) TestAuditOutputDuplicateCallbackNoDoubleRefund() {
	ctx := context.Background()
	userID := int64(71008)
	sessionID := "session-risk-repeat"
	seedBalance(s.T(), s.deps.db, userID, 100)
	seedConsumedProject(s.T(), s.deps.db, s.deps.projects, ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusRunning,
		PointsStatus: creationdomain.PointStatusUsed, PointsCost: 10,
		PointsOrderID: sessionID + "_1", RetryNum: 1,
	})
	recordID := seedAuditingRecord(s.T(), s.deps.records, ctx, userID, sessionID)
	riskCb := &riskcontracts.RiskCallbackReq{
		BizID: "dujiaapp_mixvideo60s_t2v_output-1", ThirdID: sessionID, UserID: userID,
		Passed: false, Reason: "违规", Extra: map[string]string{"callback_record_id": fmt.Sprintf("%d", recordID)},
	}

	// First time: fail + refund (seed 100 - deduct 10 + refund 10 = 110)
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))
	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus)
	testx.Equal(s.T(), 110, mustBalance(s.T(), s.deps.points, ctx, userID))

	// Second time (same record): terminal short-circuit → no further refund, terminal state not overwritten
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))
	project, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus)
	testx.Equal(s.T(), 110, mustBalance(s.T(), s.deps.points, ctx, userID), "duplicate callbacks do not refund again")

	// The audit_record terminal state is not overwritten
	rec, err := s.deps.records.Get(ctx, recordID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), riskdomain.StatusFail, rec.Status)
}

// TestAuditOutputThirdIDIsProjectID the callback carries only projectID (no recordID, legacy protocol compatibility):
// audit-record reconciliation is skipped (recordID=0); creation is still notified to settle the project (the terminal state is covered by the project-side short-circuit),
// and refunds are handled idempotently on the project side.
func (s *RiskAppIntegrationSuite) TestAuditOutputThirdIDIsProjectID() {
	ctx := context.Background()
	userID := int64(71009)
	sessionID := "session-risk-legacy"
	seedBalance(s.T(), s.deps.db, userID, 100)
	seedConsumedProject(s.T(), s.deps.db, s.deps.projects, ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusRunning,
		PointsStatus: creationdomain.PointStatusUsed, PointsCost: 10,
		PointsOrderID: sessionID + "_1", RetryNum: 1,
	})

	// Extra has no callback_record_id → recordID=0; audit-record reconciliation is skipped, only creation is notified
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, &riskcontracts.RiskCallbackReq{
		BizID: "x", ThirdID: sessionID, UserID: userID, Passed: false, Reason: "无记录直接拒绝",
		Extra: map[string]string{},
	}))

	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus, "without a record the refund still settles on the project side")
	testx.Equal(s.T(), 110, mustBalance(s.T(), s.deps.points, ctx, userID), "seed 100 - deduct 10 + refund 10")
}

// --- Helpers ---

// openMySQLTestDB switches to the repository root to read config and opens the testdb MySQL connection.
func openMySQLTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..", "..")
	testx.NoError(t, os.Chdir(repoRoot))

	cfg := config.Load("config/config.json")
	cfg.MySQL.Database = "testdb"
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.MySQL.User, cfg.MySQL.Password, cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.Database)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	testx.NoError(t, err)
	return db
}

// openRedisTestDB opens the testdb Redis with a separate DB index (DB 5), avoiding pollution of the development cache.
//
// Redis DB index convention: the three integration suites share one testdb MySQL, each using a different Redis DB index
// to isolate namespaces and avoid cross-suite pollution — monitor=3, creation=4, risk=5 (blacklist keys, etc.).
// Suites run serially (go test -p 1) so DB indexes never collide; hence risk uses 5 instead of reusing the first two indexes.
func openRedisTestDB(t *testing.T) *redis.Client {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..", "..")
	testx.NoError(t, os.Chdir(repoRoot))

	cfg := config.Load("config/config.json")
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr(), Password: cfg.Redis.Password, DB: 5})
	testx.NoError(t, rdb.Ping(context.Background()).Err())
	return rdb
}

// buildRiskDeps assembles the new-architecture components (aligned with internal/app/app.go's New, focused on the risk application layer).
//
// Isomorphic to the creation integration tests' buildCreationDeps, but using "words that do appear" (the default list) to
// verify the interception path; dependency inversion: CompensationSink / AuditResultSink are implemented and injected
// by creation.infrastructure.
func buildRiskDeps(t *testing.T, db *gorm.DB, rdb *redis.Client) *riskDeps {
	t.Helper()

	// points context
	pointsRepo := pointsinfra.NewPointsRepositoryImpl(db)
	pointsApp := pointsapp.NewPointsApp(pointsRepo)

	// risk context (local sensitive-word backend)
	riskRecords := riskinfra.NewAuditRecordRepositoryImpl(db)
	auditBackend := riskinfra.NewLocalAuditBackend()

	// creation context
	projRepo := creationinfra.NewProjectRepositoryImpl(db)
	compRepo := creationinfra.NewCompensationRepositoryImpl(db)

	// Dependency inversion: creation implements the risk-side AuditResultSink / CompensationSink and injects them
	compSink := creationinfra.NewCompensationSinkImpl(compRepo)
	onAuditResult := creationinfra.NewOnAuditResultImpl(projRepo, pointsApp)
	blacklistChecker := riskinfra.NewRedisBlacklistChecker(rdb)
	riskAuditApp := riskapp.NewRiskAuditApp(auditBackend, riskRecords, compSink, blacklistChecker)
	auditOutputApp := riskapp.NewAuditOutputApp(riskRecords, onAuditResult)

	return &riskDeps{
		db:           db,
		rdb:          rdb,
		riskAudit:    riskAuditApp,
		auditOutput:  auditOutputApp,
		records:      riskRecords,
		projects:     projRepo,
		comp:         compRepo,
		points:       *pointsApp,
		auditBackend: auditBackend,
	}
}

// seedBalance gives the user an initial balance (flow_type=2 credit, equivalent to the old seedBalance).
func seedBalance(t *testing.T, db *gorm.DB, userID int64, amount int) {
	t.Helper()
	now := time.Now().Unix()
	testx.NoError(t, db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, 'seed', ?, 2, ?, 'test seed', 1, ?)",
		userID, fmt.Sprintf("risk_seed_%d", userID), amount, now,
	).Error)
}

// seedConsumedProject creates a project with points deducted + the matching deduction entry (equivalent to the post-create state).
// PointsCost and PointsOrderID must be non-zero: deduction entries are unique by points_order_id, and refunds reconcile by it.
func seedConsumedProject(t *testing.T, db *gorm.DB, projects creationdomain.ProjectRepository, ctx context.Context, p *creationdomain.Project) {
	t.Helper()
	now := time.Now().Unix()
	testx.NoError(t, db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, ?, ?, 1, ?, 'consume', 1, ?)",
		p.UserID, p.ProjectID, p.PointsOrderID, p.PointsCost, now,
	).Error)
	testx.NoError(t, projects.Insert(ctx, p))
}

// seedAuditingRecord inserts an auditing audit record (equivalent to the post-successful-AsyncAudit state).
func seedAuditingRecord(t *testing.T, records riskdomain.AuditRecordRepository, ctx context.Context, userID int64, projectID string) uint64 {
	t.Helper()
	id, err := records.Insert(ctx, &riskdomain.AuditRecord{
		UserID: userID, ProjectID: projectID,
		TaskType: "risk:async:" + riskdomain.MixVideo60sT2VOutputSubproduct,
		Status:   riskdomain.StatusAuditing,
	})
	testx.NoError(t, err)
	return id
}

// mustAtoiID parses the string callback_record_id in extra into uint64.
func mustAtoiID(t *testing.T, s string) uint64 {
	t.Helper()
	id, err := strconv.ParseUint(s, 10, 64)
	testx.NoError(t, err)
	return id
}

// mustBalance queries the user balance (assertion helper).
func mustBalance(t *testing.T, points pointsapp.PointsApp, ctx context.Context, userID int64) int {
	t.Helper()
	b, err := points.GetBalance(ctx, userID)
	testx.NoError(t, err)
	return b
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func firstNonNil(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func TestRiskAppIntegrationSuite(t *testing.T) {
	suite.Run(t, new(RiskAppIntegrationSuite))
}
