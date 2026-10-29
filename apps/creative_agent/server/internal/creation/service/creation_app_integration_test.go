//go:build integration

// Package application_test creation application-service integration tests: against real MySQL + Redis.
//
// How to run (requires MySQL/Redis):
//
//	cd base-server && go test -tags=integration ./internal/creation/... -count=1 -v
//
// Covers the 5 core chains of Task 6.1 (migrated from the old phase4/phase5 integration-test behavior,
// reassembled with the new DDD architecture components):
//   - Instant-image sync chain: pre_create → create → sync_post_create → done; points deducted exactly once
//   - Mix-cut full chain: scriptwriter pre_create → async_post_create → scriptwriter callback → final-video pre_create →
//     create → async_post_create → final-video callback → audit intermediate state → risk callback passed → done; points deducted once, never refunded
//   - Terminal short-circuit: duplicate callbacks after done/fail are not processed; terminal states are never overwritten
//   - Idempotency: repeated create never deducts twice; repeated risk callbacks never refund twice
//   - Timeout fallback: running timeout → refund points + fail (RefundAndFailTimeout)
//
// Assembly (aligned with internal/app/app.go's New): the local sensitive-word backend (LocalAuditBackend,
// with the word list replaced by "words that never appear" so all chain content passes) + real points/risk/creation repositories +
// the Redis blacklist checker. Callback dispatch goes through CallbackDispatcher (goroutine async execution; tests poll for terminal states).
package service_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	authinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/auth/infrastructure"
	creationcontracts "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	creationinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/dao"
	creationdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	creationapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/service"
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

// creationDeps holds the test-assembled new-architecture components (equivalent to internal/app/app.go's New output).
type creationDeps struct {
	db          *gorm.DB
	rdb         *redis.Client
	creationApp *creationapp.CreationApp // facade of the four use cases
	dispatcher  *creationinfra.CallbackDispatcher
	auditOutput *riskapp.AuditOutputApp // risk-callback terminal settling
	projects    creationdomain.ProjectRepository
	records     creationdomain.CallbackRecordRepository
	riskAudit   riskdomain.AuditRecordRepository
	points      pointsapp.PointsApp
}

// CreationAppIntegrationSuite is the integration test suite.
type CreationAppIntegrationSuite struct {
	suite.Suite
	deps *creationDeps
}

func (s *CreationAppIntegrationSuite) SetupSuite() {
	t := s.T()
	db := mustOpenMySQL(t)
	rdb := mustOpenRedis(t)

	// AutoMigrate each bounded context's schema (idempotent; missing columns are added)
	testx.NoError(t, authinfra.Migrate(db))
	testx.NoError(t, creationinfra.Migrate(db))
	testx.NoError(t, pointsinfra.Migrate(db))
	testx.NoError(t, riskinfra.Migrate(db))

	s.deps = buildCreationDeps(t, db, rdb)
}

func (s *CreationAppIntegrationSuite) SetupTest() {
	t := s.T()
	testx.NoError(t, s.deps.db.Exec("TRUNCATE TABLE cg_creation_project").Error)
	testx.NoError(t, s.deps.db.Exec("TRUNCATE TABLE cg_task_callback_record").Error)
	testx.NoError(t, s.deps.db.Exec("TRUNCATE TABLE cg_points_flow").Error)
	testx.NoError(t, s.deps.db.Exec("TRUNCATE TABLE cg_audit_compensation").Error)
	testx.NoError(t, s.deps.db.Exec("TRUNCATE TABLE cg_risk_audit_record").Error)
	testx.NoError(t, s.deps.rdb.FlushDB(context.Background()).Err())
}

// TestMiaotuSyncChain is the instant-image sync chain: pre_create → create → sync_post_create → done; points deducted exactly once.
func (s *CreationAppIntegrationSuite) TestMiaotuSyncChain() {
	ctx := context.Background()
	userID := int64(810001)
	sessionID := "session-integration-miaotu-sync"
	seedBalance(s.T(), s.deps.db, userID, 100)

	// 1. pre_create (role=4 visual designer / instant image) — input risk control passes
	pre, err := s.deps.creationApp.PreCreate.HandlePreCreate(ctx, &creationcontracts.PreCreateReq{
		UserID:       userID,
		SessionID:    sessionID,
		TaskStepID:   4001,
		ProjectType:  creationdomain.ProjectTypeMiaotuImage,
		RoleType:     creationdomain.RoleTypeVisualDesigner,
		ProjectInput: `{"input":"{\"query\":\"画一只猫\"}"}`,
	})
	testx.NoError(s.T(), err)
	testx.NotNil(s.T(), pre.Data)
	testx.Equal(s.T(), 0, pre.Data.Status, "instant-image input risk control passes")

	// 2. create deducts points (instant-image unit price 5)
	create, err := s.deps.creationApp.Create.HandleCreate(ctx, &creationcontracts.CreateReq{
		UserID:       userID,
		SessionID:    sessionID,
		ProjectType:  creationdomain.ProjectTypeMiaotuImage,
		ProjectInput: map[string]any{"query": "画一只猫"},
	})
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), "success", create.Data.Status)

	projects, err := s.deps.projects.GetBySessionID(ctx, sessionID, userID)
	testx.NoError(s.T(), err)
	testx.Len(s.T(), projects, 1)
	project := projects[0]
	testx.Equal(s.T(), creationdomain.ProjectStatusRunning, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusUsed, project.PointsStatus)

	// 3. sync_post_create: one step to terminal done (image risk control → text risk control → all pass)
	specialInfo := base64.StdEncoding.EncodeToString([]byte(`{"images":["http://img/a.png","http://img/b.png"],"title":"一只猫"}`))
	sync, err := s.deps.creationApp.PostCreate.HandleSyncPostCreate(ctx, &creationcontracts.SyncPostCreateReq{
		SessionID:  sessionID,
		UserID:     userID,
		RoleType:   creationdomain.RoleTypeVisualDesigner,
		TaskOutput: buildSkillTaskOutput(0, "success", 88001, specialInfo),
		Status:     creationdomain.CallbackTaskStatusSuccess,
	})
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), "success", sync.Data.Status)
	testx.Equal(s.T(), creationdomain.CardTypeImageDelivery, sync.Data.TaskArguments["card_type"])

	projects, err = s.deps.projects.GetBySessionID(ctx, sessionID, userID)
	testx.NoError(s.T(), err)
	testx.Len(s.T(), projects, 1)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, projects[0].Status, "instant image reaches terminal in one sync step")
	testx.Equal(s.T(), project.ID, projects[0].ID, "there is exactly one project throughout")

	// Points deducted once (100 - 5 = 95), never refunded
	testx.Equal(s.T(), 95, s.mustBalance(ctx, userID), "instant image deducts 5 points")
	flows, err := s.deps.points.ListByProject(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Len(s.T(), flows, 1, "there is exactly one points-ledger entry")
	testx.Equal(s.T(), pointsdomain.FlowTypeConsume, flows[0].FlowType, "it is a deduction entry")
}

// TestMixVideoFullChain is the mix-cut full chain: scriptwriter → final video → audit intermediate state → risk callback passes → done;
// points deducted once, never refunded; includes idempotency and short-circuit assertions.
func (s *CreationAppIntegrationSuite) TestMixVideoFullChain() {
	ctx := context.Background()
	userID := int64(810002)
	sessionID := "session-integration-mixvideo-full"
	seedBalance(s.T(), s.deps.db, userID, 100)

	// 1. Scriptwriter pre_create (role=3, input risk control passes)
	screenplayPre, err := s.deps.creationApp.PreCreate.HandlePreCreate(ctx, &creationcontracts.PreCreateReq{
		UserID:       userID,
		SessionID:    sessionID,
		ParentStepID: 1001,
		TaskStepID:   1002,
		ProjectType:  creationdomain.ProjectTypeMixVideo,
		RoleType:     creationdomain.RoleTypeScreenplay,
		ProjectInput: `{"input":"{\"query\":\"写一个关于夏天的故事\"}"}`,
	})
	testx.NoError(s.T(), err)
	testx.NotNil(s.T(), screenplayPre.Data)
	testx.Equal(s.T(), 0, screenplayPre.Data.Status, "scriptwriter input risk control passes")

	// 2. Scriptwriter async_post_create: register the pipeline ID into callback_record (no project-table write)
	_, err = s.deps.creationApp.PostCreate.HandleAsyncPostCreate(ctx, &creationcontracts.AsyncPostCreateReq{
		SessionID:   sessionID,
		UserID:      userID,
		RoleType:    creationdomain.RoleTypeScreenplay,
		ProjectType: creationdomain.ProjectTypeMixVideo,
		TaskOutput:  buildSkillTaskOutput(0, "success", 99001, `{"script":"夏天的故事剧本"}`),
		Status:      creationdomain.CallbackTaskStatusSuccess,
	})
	testx.NoError(s.T(), err)

	projects, err := s.deps.projects.GetBySessionID(ctx, sessionID, userID)
	testx.NoError(s.T(), err)
	testx.Len(s.T(), projects, 0, "the scriptwriter stage writes no project row")

	scriptRecord, err := s.deps.records.GetByProjectAndType(ctx, sessionID, "3:mix_video")
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusRunning, scriptRecord.Status)

	// 3. Scriptwriter callback (task_detail, task_type=3:mix_video) → segment risk control passes → record=done
	s.deps.dispatcher.Dispatch(ctx, &creationcontracts.CallbackReq{
		TaskID:       99001,
		ProjectID:    sessionID,
		UserID:       userID,
		TaskStatus:   creationdomain.CallbackTaskStatusSuccess,
		SpecialInfo:  json.RawMessage(`{"script":"夏天的故事剧本"}`),
		ExtendParams: creationcontracts.BuildExtendParamsJSON("3:mix_video", int64(scriptRecord.ID), "only", 1002, 1001),
	})
	s.awaitRecordStatus(ctx, scriptRecord.ID, creationdomain.ProjectStatusDone)

	// 4. Final-video pre_create (role=5, input risk control passes)
	editorPre, err := s.deps.creationApp.PreCreate.HandlePreCreate(ctx, &creationcontracts.PreCreateReq{
		UserID:       userID,
		SessionID:    sessionID,
		ParentStepID: 4001,
		TaskStepID:   5001,
		ProjectType:  creationdomain.ProjectTypeMixVideo,
		RoleType:     creationdomain.RoleTypeEditor,
		ProjectInput: `{"input":"{\"query\":\"把剧本做成视频\"}"}`,
	})
	testx.NoError(s.T(), err)
	testx.NotNil(s.T(), editorPre.Data)
	testx.Equal(s.T(), 0, editorPre.Data.Status, "final-video input risk control passes")

	// 5. Final-video create (deduct 10 points + create the project)
	create, err := s.deps.creationApp.Create.HandleCreate(ctx, &creationcontracts.CreateReq{
		UserID:       userID,
		SessionID:    sessionID,
		ProjectType:  creationdomain.ProjectTypeMixVideo,
		ProjectInput: map[string]any{"query": "把剧本做成视频"},
		Title:        "我的混剪",
	})
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), "success", create.Data.Status)

	projects, err = s.deps.projects.GetBySessionID(ctx, sessionID, userID)
	testx.NoError(s.T(), err)
	testx.Len(s.T(), projects, 1, "there is exactly one project throughout")
	project := projects[0]
	pointsOrderID := project.PointsOrderID
	testx.Equal(s.T(), creationdomain.ProjectStatusRunning, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusUsed, project.PointsStatus)
	testx.Equal(s.T(), 10, project.PointsCost, "the mix-cut unit price is 10")

	// 6. Final-video async_post_create: register the pipeline ID into the project
	_, err = s.deps.creationApp.PostCreate.HandleAsyncPostCreate(ctx, &creationcontracts.AsyncPostCreateReq{
		SessionID:   sessionID,
		UserID:      userID,
		RoleType:    creationdomain.RoleTypeEditor,
		ProjectType: creationdomain.ProjectTypeMixVideo,
		TaskOutput:  buildSkillTaskOutput(0, "success", 99002, ""),
		Status:      creationdomain.CallbackTaskStatusSuccess,
	})
	testx.NoError(s.T(), err)
	projects, err = s.deps.projects.GetBySessionID(ctx, sessionID, userID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), "99002", projects[0].ProductionLineID, "the pipeline ID is registered")

	// 7. Final-video callback (task_detail, task_type=5:mix_video) → set audit first (not done!)
	s.deps.dispatcher.Dispatch(ctx, &creationcontracts.CallbackReq{
		TaskID:       99002,
		ProjectID:    sessionID,
		UserID:       userID,
		TaskStatus:   creationdomain.CallbackTaskStatusSuccess,
		SpecialInfo:  json.RawMessage(`{"script":"夏天的故事剧本","description":"一段成片视频"}`),
		ExtendParams: creationcontracts.BuildExtendParamsJSON("5:mix_video", 0, "multi", 5001, 4001),
	})
	// ⚠️ The callback handler runs asynchronously in a goroutine: waiting for the audit status is not enough —
	// you must first wait for ext.async_audit_record_id to be written (the handler's last action),
	// otherwise the subsequent risk callback races with the goroutine's closing Update and overwrites the done terminal state.
	asyncRecordID := s.awaitAsyncAuditRecordID(ctx, sessionID)

	p, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusAudit, p.Status, "a successful final-video output sets audit first, not done")
	testx.Greater(s.T(), asyncRecordID, int64(0), "the async risk-control recordID is passed through")

	// 8. Risk callback (passed=true) → the audit record settles done + the project is set done, no refund
	riskCb := &riskcontracts.RiskCallbackReq{
		BizID:     "dujiaapp_mixvideo60s_t2v_output-99002",
		ThirdID:   sessionID,
		UserID:    userID,
		Passed:    true,
		AuditType: "video",
		Extra:     map[string]string{"callback_record_id": fmt.Sprintf("%d", asyncRecordID)},
	}
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))

	p, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, p.Status, "risk passed → done")
	testx.Equal(s.T(), creationdomain.PointStatusUsed, p.PointsStatus, "no refund")

	// Points deducted once (100 - 10 = 90), no refunds throughout
	testx.Equal(s.T(), 90, s.mustBalance(ctx, userID), "10 points deducted exactly once overall, no refund")
	flows, err := s.deps.points.ListByProject(ctx, sessionID)
	testx.NoError(s.T(), err)
	var consumeN, refundN int
	for _, f := range flows {
		switch f.FlowType {
		case pointsdomain.FlowTypeConsume:
			consumeN++
		case pointsdomain.FlowTypeRefund:
			refundN++
		}
	}
	testx.Equal(s.T(), 1, consumeN, "points deducted exactly once")
	testx.Equal(s.T(), 0, refundN, "no points refunded")

	// 9. Repeated create (idempotent): the session already has a project → returns success, no double deduction
	create2, err := s.deps.creationApp.Create.HandleCreate(ctx, &creationcontracts.CreateReq{
		UserID:       userID,
		SessionID:    sessionID,
		ProjectType:  creationdomain.ProjectTypeMixVideo,
		ProjectInput: map[string]any{"query": "把剧本做成视频"},
	})
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), "success", create2.Data.Status, "a repeated create idempotently returns success")
	testx.Equal(s.T(), 90, s.mustBalance(ctx, userID), "a repeated create does not deduct again")
	projects, err = s.deps.projects.GetBySessionID(ctx, sessionID, userID)
	testx.NoError(s.T(), err)
	testx.Len(s.T(), projects, 1, "a repeated create creates no new project")

	// 10. Repeated risk callback (same audit record): terminal short-circuit → no refund, terminal state not overwritten
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))
	p, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, p.Status, "the terminal state is not overwritten")
	testx.Equal(s.T(), creationdomain.PointStatusUsed, p.PointsStatus, "a repeated risk callback does not refund again")
	testx.Equal(s.T(), 90, s.mustBalance(ctx, userID), "a repeated risk callback does not refund")
	testx.Equal(s.T(), pointsOrderID, p.PointsOrderID, "points_order_id is unchanged")
}

// TestCreateIdempotentNoDoubleDeduct idempotency: repeated create on the same session never deducts twice.
func (s *CreationAppIntegrationSuite) TestCreateIdempotentNoDoubleDeduct() {
	ctx := context.Background()
	userID := int64(810003)
	sessionID := "session-integration-create-idempotent"
	seedBalance(s.T(), s.deps.db, userID, 100)

	req := &creationcontracts.CreateReq{
		UserID:       userID,
		SessionID:    sessionID,
		ProjectType:  creationdomain.ProjectTypeMiaotuImage,
		ProjectInput: map[string]any{"query": "画一只猫"},
	}
	first, err := s.deps.creationApp.Create.HandleCreate(ctx, req)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), "success", first.Data.Status)
	testx.Equal(s.T(), 95, s.mustBalance(ctx, userID), "the first deduction is 5 points")

	// Repeated create: the session already has a project → idempotently returns success, no further deduction
	second, err := s.deps.creationApp.Create.HandleCreate(ctx, req)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), "success", second.Data.Status)
	testx.Equal(s.T(), 95, s.mustBalance(ctx, userID), "a repeated create does not deduct again")

	projects, err := s.deps.projects.GetBySessionID(ctx, sessionID, userID)
	testx.NoError(s.T(), err)
	testx.Len(s.T(), projects, 1, "a repeated create creates no new project")
}

// TestRepeatedRiskCallbackNoDoubleRefund idempotency: repeated risk callbacks on the same audit record refund only once.
func (s *CreationAppIntegrationSuite) TestRepeatedRiskCallbackNoDoubleRefund() {
	ctx := context.Background()
	userID := int64(810004)
	sessionID := "session-integration-risk-repeat"
	seedBalance(s.T(), s.deps.db, userID, 100)

	// Create a project with points deducted (create deducted 10, balance 90), simulating the final-video callback having set audit
	_, err := s.deps.creationApp.Create.HandleCreate(ctx, &creationcontracts.CreateReq{
		UserID:       userID,
		SessionID:    sessionID,
		ProjectType:  creationdomain.ProjectTypeMixVideo,
		ProjectInput: map[string]any{"query": "做个混剪"},
	})
	testx.NoError(s.T(), err)
	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.True(s.T(), project.MarkAudit())
	testx.NoError(s.T(), s.deps.projects.Update(ctx, project))

	// Insert one auditing audit record (the artifact of AsyncAudit)
	recordID, err := s.deps.riskAudit.Insert(ctx, &riskdomain.AuditRecord{
		UserID:    userID,
		ProjectID: sessionID,
		TaskType:  "risk:async:dujiaapp_mixvideo60s_t2v_output",
		Status:    riskdomain.StatusAuditing,
	})
	testx.NoError(s.T(), err)

	riskCb := &riskcontracts.RiskCallbackReq{
		BizID:   "dujiaapp_mixvideo60s_t2v_output-1",
		ThirdID: sessionID,
		UserID:  userID,
		Passed:  false,
		Reason:  "视频包含违规内容",
		Extra:   map[string]string{"callback_record_id": fmt.Sprintf("%d", recordID)},
	}

	// First time: refund points + fail
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))
	project, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus)
	testx.Equal(s.T(), 110, s.mustBalance(ctx, userID), "the mix-cut 10 points are refunded (seed 100 + refund 10)")

	// Second time: audit-record terminal short-circuit + project terminal short-circuit → no further refund
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))
	testx.Equal(s.T(), 110, s.mustBalance(ctx, userID), "a repeated risk callback does not refund again")
}

// TestCallbackAfterTerminalNoOp terminal short-circuit: duplicate callbacks after done/fail are not processed; terminal states are not overwritten.
func (s *CreationAppIntegrationSuite) TestCallbackAfterTerminalNoOp() {
	ctx := context.Background()
	userID := int64(810005)
	sessionID := "session-integration-cb-terminal"
	seedBalance(s.T(), s.deps.db, userID, 100)

	// Create a done project (points deducted, not refunded)
	testx.NoError(s.T(), s.deps.projects.Insert(ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusDone,
		PointsStatus: creationdomain.PointStatusUsed, PointsCost: 10,
		PointsOrderID: sessionID + "_1", RetryNum: 1,
	}))

	// Repeated final-video callback (success) after done → the handler short-circuits on terminal; the status is unchanged
	s.deps.dispatcher.Dispatch(ctx, &creationcontracts.CallbackReq{
		TaskID:       99001,
		ProjectID:    sessionID,
		UserID:       userID,
		TaskStatus:   creationdomain.CallbackTaskStatusSuccess,
		SpecialInfo:  json.RawMessage(`{"script":"剧本","description":"视频"}`),
		ExtendParams: creationcontracts.BuildExtendParamsJSON("5:mix_video", 0, "multi", 5001, 4001),
	})
	// Callbacks run asynchronously in goroutines; wait briefly for the handler to finish (no state change; polling suffices)
	s.awaitProjectStatus(ctx, sessionID, creationdomain.ProjectStatusDone)

	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, project.Status, "the terminal state is not overwritten by callbacks")
	testx.Equal(s.T(), creationdomain.PointStatusUsed, project.PointsStatus, "no repeated refunds after terminal")

	// Risk callback !passed after done → terminal short-circuit; no refund, no overwrite
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, &riskcontracts.RiskCallbackReq{
		BizID: "x", ThirdID: sessionID, UserID: userID, Passed: false, Reason: "违规",
	}))
	project, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusUsed, project.PointsStatus)
	testx.Equal(s.T(), 100, s.mustBalance(ctx, userID), "the seed 100 is untouched (never deducted, never refunded)")
}

// TestRefundAndFailTimeout timeout fallback: running timeout → refund points + fail; a second fallback on an already-failed project is a no-op.
func (s *CreationAppIntegrationSuite) TestRefundAndFailTimeout() {
	ctx := context.Background()
	userID := int64(810006)
	sessionID := "session-integration-timeout"
	seedBalance(s.T(), s.deps.db, userID, 100)
	now := time.Now().Unix()

	// Create the deduction ledger entry (simulating create having deducted 10 points)
	testx.NoError(s.T(), s.deps.db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, ?, ?, 1, 10, 'consume', 1, ?)",
		userID, sessionID, sessionID+"_1", now,
	).Error)

	// Create a project created before the timeout window, still running with points deducted
	stuckTime := time.Now().Add(-25 * time.Hour).Unix()
	testx.NoError(s.T(), s.deps.projects.Insert(ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusRunning,
		PointsStatus: creationdomain.PointStatusUsed, PointsCost: 10,
		PointsOrderID: sessionID + "_1", RetryNum: 1,
		CreateTime: stuckTime, UpdateTime: stuckTime,
	}))

	// Timeout fallback: refund points + fail
	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.NoError(s.T(), s.deps.creationApp.PostCreate.RefundAndFailTimeout(ctx, project))

	project, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status, "timeout sets fail")
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus, "timeout refunds points")
	testx.Equal(s.T(), 110, s.mustBalance(ctx, userID), "seed 100 + refund 10 = 110")

	// Idempotent: another timeout fallback on an already-failed project → no-op, no repeated refund
	testx.NoError(s.T(), s.deps.creationApp.PostCreate.RefundAndFailTimeout(ctx, project))
	testx.Equal(s.T(), 110, s.mustBalance(ctx, userID), "a repeated timeout fallback does not refund again")
}

// --- Helpers ---

// mustBalance queries the user balance (assertion helper).
func (s *CreationAppIntegrationSuite) mustBalance(ctx context.Context, userID int64) int {
	s.T().Helper()
	b, err := s.deps.points.GetBalance(ctx, userID)
	testx.NoError(s.T(), err)
	return b
}

// awaitRecordStatus polls the callback record status until target (the callback handler runs asynchronously in a goroutine).
func (s *CreationAppIntegrationSuite) awaitRecordStatus(ctx context.Context, id uint64, target string) {
	s.T().Helper()
	for i := 0; i < 50; i++ {
		record, err := s.deps.records.Get(ctx, id)
		testx.NoError(s.T(), err)
		if record.Status == target {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.T().Fatalf("record %d did not reach status %s within the timeout", id, target)
}

// awaitProjectStatus polls the work status until target.
func (s *CreationAppIntegrationSuite) awaitProjectStatus(ctx context.Context, projectID string, target string) {
	s.T().Helper()
	for i := 0; i < 50; i++ {
		project, err := s.deps.projects.GetByProjectID(ctx, projectID)
		testx.NoError(s.T(), err)
		if project.Status == target {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.T().Fatalf("project %s did not reach status %s within the timeout", projectID, target)
}

// awaitAsyncAuditRecordID polls for the async risk-control recordID write in project.ext.
//
// ⚠️ The final-video callback handler runs asynchronously in a goroutine; after setting audit it also starts async video risk control
// and passes the recordID into ext — the recordID write is the handler's last action. Only after it completes
// can the risk callback be safely simulated (otherwise the goroutine's closing Update races with the risk callback's settling Update).
func (s *CreationAppIntegrationSuite) awaitAsyncAuditRecordID(ctx context.Context, projectID string) int64 {
	s.T().Helper()
	for i := 0; i < 50; i++ {
		project, err := s.deps.projects.GetByProjectID(ctx, projectID)
		testx.NoError(s.T(), err)
		ext, err := creationdomain.ParseExt(project.Ext)
		testx.NoError(s.T(), err)
		if ext.AsyncAuditRecordID > 0 {
			return ext.AsyncAuditRecordID
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.T().Fatalf("the async risk-control recordID of project %s was not written within the timeout", projectID)
	return 0
}

// mustOpenMySQL switches to the repository root to read config and opens the testdb MySQL connection.
func mustOpenMySQL(t *testing.T) *gorm.DB {
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

// mustOpenRedis opens the testdb Redis (separate DB, avoiding pollution of the development cache).
func mustOpenRedis(t *testing.T) *redis.Client {
	t.Helper()
	cfg := config.Load("config/config.json")
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr(), Password: cfg.Redis.Password, DB: 4})
	testx.NoError(t, rdb.Ping(context.Background()).Err())
	return rdb
}

// buildCreationDeps assembles the new-architecture components (aligned with internal/app/app.go's New, but with the local sensitive-word backend).
//
// ⚠️ Key point: LocalAuditBackend.Submit falls back to the default sensitive-word list (DefaultSensitiveWords)
// when the word list is empty, so "words that never appear" are used instead of an empty list, letting all chain content pass.
func buildCreationDeps(t *testing.T, db *gorm.DB, rdb *redis.Client) *creationDeps {
	t.Helper()

	// points context
	pointsRepo := pointsinfra.NewPointsRepositoryImpl(db)
	pointsApp := pointsapp.NewPointsApp(pointsRepo)

	// risk context (local sensitive-word backend, content passes)
	riskRecords := riskinfra.NewAuditRecordRepositoryImpl(db)
	auditBackend := riskinfra.NewLocalAuditBackend()
	auditBackend.SetSensitiveWords([]string{"__no_such_sensitive_word__"})

	// creation context
	projRepo := creationinfra.NewProjectRepositoryImpl(db)
	cbRepo := creationinfra.NewCallbackRecordRepositoryImpl(db)
	compRepo := creationinfra.NewCompensationRepositoryImpl(db)

	// Dependency inversion: creation implements the risk-side AuditResultSink / CompensationSink and injects them
	compSink := creationinfra.NewCompensationSinkImpl(compRepo)
	onAuditResult := creationinfra.NewOnAuditResultImpl(projRepo, pointsApp)
	blacklistChecker := riskinfra.NewRedisBlacklistChecker(rdb)
	riskAuditApp := riskapp.NewRiskAuditApp(auditBackend, riskRecords, compSink, blacklistChecker)
	auditOutputApp := riskapp.NewAuditOutputApp(riskRecords, onAuditResult)

	// creation application facade + callback dispatcher
	creationApp := creationapp.NewCreationApp(projRepo, cbRepo, riskAuditApp, pointsApp)
	dispatcher := creationinfra.NewCallbackDispatcher(
		creationinfra.NewCallbackDeps(projRepo, cbRepo, compRepo, riskAuditApp, pointsApp),
	)

	return &creationDeps{
		db:          db,
		rdb:         rdb,
		creationApp: creationApp,
		dispatcher:  dispatcher,
		auditOutput: auditOutputApp,
		projects:    projRepo,
		records:     cbRepo,
		riskAudit:   riskRecords,
		points:      *pointsApp,
	}
}

// seedBalance gives the user an initial balance (flow_type=2 credit, equivalent to the old seedBalance).
func seedBalance(t *testing.T, db *gorm.DB, userID int64, amount int) {
	t.Helper()
	now := time.Now().Unix()
	testx.NoError(t, db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, 'seed', ?, 2, ?, 'test seed', 1, ?)",
		userID, fmt.Sprintf("creation_seed_%d", userID), amount, now,
	).Error)
}

// buildSkillTaskOutput builds the task_output JSON returned by operators.
func buildSkillTaskOutput(errno int, status string, taskID int64, specialInfo string) string {
	b, _ := json.Marshal(map[string]any{
		"errno": errno,
		"data": map[string]any{
			"task_id":      taskID,
			"task_status":  status,
			"special_info": specialInfo,
			"fail_reason":  "",
		},
	})
	return string(b)
}

func TestCreationAppIntegrationSuite(t *testing.T) {
	suite.Run(t, new(CreationAppIntegrationSuite))
}
