//go:build integration

// Package application_test risk 应用服务集成测试：走真实 MySQL + Redis。
//
// 运行方式（需 MySQL/Redis 可用）：
//
//	cd base-server && go test -tags=integration -p 1 ./internal/risk/... -count=1 -v
//
// 覆盖 Task 6.2 的送审 + 终态落定闭环（risk 应用层，经过真实 gorm 仓储）：
//   - 同步送审 SyncAudit：文本通过 / 敏感词拦截（本地后端显式设置敏感词表）
//   - 异步送审 AsyncAudit：插 auditing 记录 + task_type=risk:async:{subProduct} + 提交成功写 task_input
//   - 异步送审提交失败：写 cg_audit_compensation 补偿记录（biz_type=1）；补偿重试
//     RetryAsyncAudit 复用原始 auditing 记录重新提交
//   - 回调终态落定 AuditOutputApp：passed → audit_record=done + project=done（不退积分）；
//     !passed → audit_record=fail + project=fail + 退积分（幂等）；重复回调终态短路不重复退
//   - 同步合并记录器 SyncAuditRecorder：多次 Run + Finish 合并落一条记录（真实仓储）
//
// 装配（对齐 internal/app/app.go 的 New）：本地敏感词后端（LocalAuditBackend，
// 敏感词表显式设置）+ 真实 points/risk/creation 仓储 + Redis 黑名单检查器。
// 治理闭环（消费者/评估 job/黑名单）已由 internal/monitor/risk_governance_integration_test.go
// 覆盖，本文件不重复。
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

// riskDeps 是测试装配好的新架构组件（等价 internal/app/app.go 的 New 产物，聚焦 risk 侧）。
type riskDeps struct {
	db           *gorm.DB
	rdb          *redis.Client
	riskAudit    *riskapp.RiskAuditApp
	auditOutput  *riskapp.AuditOutputApp
	records      riskdomain.AuditRecordRepository // 审计记录仓储（断言用）
	projects     creationdomain.ProjectRepository
	comp         creationdomain.CompensationRepository
	points       pointsapp.PointsApp
	auditBackend *riskinfra.LocalAuditBackend // 测试可覆写敏感词表
}

// RiskAppIntegrationSuite 集成测试套件。
type RiskAppIntegrationSuite struct {
	suite.Suite
	deps *riskDeps
}

func (s *RiskAppIntegrationSuite) SetupSuite() {
	t := s.T()
	db := openMySQLTestDB(t)
	rdb := openRedisTestDB(t)

	// 各 bounded context 表结构 AutoMigrate（幂等，缺列才补）
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
	// 每个用例重置敏感词表（默认含"赌博"等，防止用例间状态泄漏）
	s.deps.auditBackend.SetSensitiveWords(riskinfra.DefaultSensitiveWords)
}

// TestSyncAuditPassAndBlock 同步送审：干净文本通过；命中敏感词拦截并给出原因。
func (s *RiskAppIntegrationSuite) TestSyncAuditPassAndBlock() {
	ctx := context.Background()
	userID := int64(71001)
	s.deps.auditBackend.SetSensitiveWords([]string{"赌博", "代考"})

	// 干净文本 → 通过
	passed, reason, err := s.deps.riskAudit.SyncAudit(ctx, riskdomain.NewRiskSubmitRequest(userID, "p-sync-pass", "b-sync-1", "今天天气不错"), "5:mix_video")
	testx.NoError(s.T(), err)
	testx.True(s.T(), passed)
	testx.Empty(s.T(), reason)

	// 敏感词文本 → 拦截，reason 含命中词
	passed, reason, err = s.deps.riskAudit.SyncAudit(ctx, riskdomain.NewRiskSubmitRequest(userID, "p-sync-block", "b-sync-2", "这里有个赌博网站"), "5:mix_video")
	testx.NoError(s.T(), err)
	testx.False(s.T(), passed)
	testx.Contains(s.T(), reason, "赌博")
}

// TestAsyncAuditWritesAuditingRecord 异步送审：插 auditing 记录 + task_type=risk:async:{subProduct}；
// 提交成功把提交入参（sub_product + 回调纽带）写进 task_input，callback_url 回填到请求。
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

	// 库里有且仅有一条 auditing 记录，task_type 含 subProduct
	rec, err := s.deps.records.Get(ctx, uint64(recordID))
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), int64(userID), rec.UserID)
	testx.Equal(s.T(), "p-async-ok", rec.ProjectID)
	testx.Equal(s.T(), "risk:async:"+subProduct, rec.TaskType)
	testx.Equal(s.T(), riskdomain.StatusAuditing, rec.Status, "异步送审插入即 auditing")

	// 回调纽带回填进请求 + task_input 记录了提交入参
	testx.Equal(s.T(), callbackURL, req.CallbackURL)
	testx.Equal(s.T(), fmt.Sprintf("%d", recordID), req.Extra["callback_record_id"])
	testx.Contains(s.T(), rec.TaskInput, subProduct)
	testx.Contains(s.T(), rec.TaskInput, callbackURL)

	// 未写补偿表（提交成功）
	_, err = s.deps.comp.GetByBizID(ctx, fmt.Sprintf("%d", recordID), creationdomain.BizTypeAsyncAudit)
	testx.ErrorIs(s.T(), err, creationdomain.ErrCompensationNotFound, "提交成功不写补偿")
}

// TestAsyncAuditSubmitFailureWritesCompensation 异步送审提交失败：空内容 → submitAuditRequest
// 校验失败 → 写一条 biz_type=1 补偿记录（复用原始 recordID），原始记录保持 auditing 供重试复用。
func (s *RiskAppIntegrationSuite) TestAsyncAuditSubmitFailureWritesCompensation() {
	ctx := context.Background()
	userID := int64(71003)
	subProduct := riskdomain.MixVideo60sT2VOutputSubproduct

	// 空内容（无文本无图片）→ 提交失败
	req := &riskdomain.RiskSubmitRequest{UserID: userID, ThirdID: "p-async-fail", BizID: "b-async-2"}
	retRecordID, err := s.deps.riskAudit.AsyncAudit(ctx, req, subProduct, "")
	testx.Error(s.T(), err)
	testx.Equal(s.T(), int64(0), retRecordID, "提交失败不返回 recordID")

	// 补偿表写了一条 biz_type=1 记录，biz_id 即原始 recordID（对账纽带）
	recordID := mustAtoiID(s.T(), req.Extra["callback_record_id"])
	comp, err := s.deps.comp.GetByBizID(ctx, fmt.Sprintf("%d", recordID), creationdomain.BizTypeAsyncAudit)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.BizTypeAsyncAudit, comp.BizType)
	testx.Equal(s.T(), creationdomain.CompensationStatusPending, comp.Status)
	testx.Equal(s.T(), userID, comp.UserID)
	// payload 含重试所需字段（供 RetryAsyncAudit 解析）
	payload := map[string]any{}
	testx.NoError(s.T(), json.Unmarshal([]byte(comp.Payload), &payload))
	testx.Equal(s.T(), subProduct, payload["sub_product"])
	testx.Equal(s.T(), "p-async-fail", payload["third_id"])
	testx.Equal(s.T(), float64(userID), payload["user_id"])

	// 原始记录仍 auditing（补偿 job 重试复用）
	rec, err := s.deps.records.Get(ctx, uint64(recordID))
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), riskdomain.StatusAuditing, rec.Status, "提交失败记录保持 auditing 供重试")
}

// TestRetryAsyncAuditDrivesFromCompensationPayload 补偿重试：从补偿 payload 驱动 RetryAsyncAudit，
// 复用原始 auditing 记录重新提交（task_input 写入提交标记），不产生第二条记录。
func (s *RiskAppIntegrationSuite) TestRetryAsyncAuditDrivesFromCompensationPayload() {
	ctx := context.Background()
	userID := int64(71004)
	subProduct := riskdomain.MixVideo60sT2VOutputSubproduct

	// 预置一条 auditing 记录 + 对应补偿记录（payload 含 content_text），等价补偿 job 扫描到的状态
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

	// 从补偿 payload 驱动重试（等价 monitor.RunCompensationJob 的 retryCompensation）
	testx.NoError(s.T(), s.deps.riskAudit.RetryAsyncAudit(ctx, map[string]any{
		"sub_product":  subProduct,
		"record_id":    int64(recordID),
		"user_id":      int64(userID),
		"third_id":     "p-retry",
		"biz_id":       "pl-retry-1",
		"callback_url": "/inner/callback/risk",
		"content_text": "一段成片视频描述",
	}))

	// 复用原始记录（仅一条）：status 仍 auditing（等风控回调），task_input 写入提交标记
	rec, err := s.deps.records.Get(ctx, recordID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), riskdomain.StatusAuditing, rec.Status)
	testx.Contains(s.T(), rec.TaskInput, `"sub_product":"`+subProduct+`"`)
	testx.Contains(s.T(), rec.TaskInput, fmt.Sprintf(`"callback_record_id":"%d"`, recordID))
}

// TestSyncAuditRecorderRealDB 同步合并记录器（真实仓储）：多次 Run + Finish 合并落一条记录，
// task_input 记录本次所有 bizID + subProduct；命中敏感词时状态为 fail。
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

	// 只有一条记录：状态 fail（任一不通过），task_type 与 bizID/subProduct 合并
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
	// 行为断言：多次 Run 合并为恰好一条记录（自增 ID 有效即可，具体自增值是数据库实现细节，不做断言）
	var recordCount int64
	testx.NoError(s.T(), s.deps.db.Raw("SELECT COUNT(*) FROM cg_risk_audit_record").Scan(&recordCount).Error)
	testx.Equal(s.T(), int64(1), recordCount, "多次 Run + Finish 合并为一条记录")
	testx.Greater(s.T(), row.ID, uint64(0), "合并记录的自增 ID 有效")
	testx.Equal(s.T(), int64(userID), row.UserID)
	testx.Equal(s.T(), projectID, row.ProjectID)
	testx.Equal(s.T(), "risk:5:mix_video:precreate", row.TaskType)
	testx.Equal(s.T(), riskdomain.StatusFail, row.Status)

	var in riskinfra.SyncAuditTaskInput
	testx.NoError(s.T(), json.Unmarshal([]byte(row.TaskInput), &in))
	testx.ElementsMatch(s.T(), []string{"b-rec-1", "b-rec-2"}, in.BizIDs)
	testx.ElementsMatch(s.T(), []string{"dujiaapp_mixvideo_t2t_input", "dujiaapp_mixvideo60s_t2v_output"}, in.SubTypes)
}

// TestAuditOutputPassedSettlesDone 回调通过落定：audit_record → done，project → done，不退积分。
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

	// 回调 passed=true
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, &riskcontracts.RiskCallbackReq{
		BizID: "dujiaapp_mixvideo60s_t2v_output-1", ThirdID: sessionID, UserID: userID,
		Passed: true, Extra: map[string]string{"callback_record_id": fmt.Sprintf("%d", recordID)},
	}))

	// audit_record → done
	rec, err := s.deps.records.Get(ctx, recordID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), riskdomain.StatusDone, rec.Status)

	// project → done，积分已扣未退
	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusUsed, project.PointsStatus)
	testx.Equal(s.T(), 90, mustBalance(s.T(), s.deps.points, ctx, userID), "通过不退积分（100-10）")
}

// TestAuditOutputNotPassedRefundsAndFails 回调不通过落定：audit_record → fail，project → fail + 退积分。
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

	// 回调 passed=false
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, &riskcontracts.RiskCallbackReq{
		BizID: "dujiaapp_mixvideo60s_t2v_output-1", ThirdID: sessionID, UserID: userID,
		Passed: false, Reason: "输出包含违规内容",
		Extra: map[string]string{"callback_record_id": fmt.Sprintf("%d", recordID)},
	}))

	// audit_record → fail
	rec, err := s.deps.records.Get(ctx, recordID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), riskdomain.StatusFail, rec.Status)

	// project → fail + 退积分（种子 100 - 扣 10 + 退 10 = 110）
	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus)
	testx.Equal(s.T(), 110, mustBalance(s.T(), s.deps.points, ctx, userID), "不通过退积分（原扣分流水置回退，退款加回余额）")

	// 只有一条退款流水（无重复退）
	flows, err := s.deps.points.ListByProject(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), 2, len(flows), "扣分 + 退款两条流水（种子入账 project_id='seed' 不在此列表）")
	var refundN int
	for _, f := range flows {
		if f.FlowType == pointsdomain.FlowTypeRefund {
			refundN++
		}
	}
	testx.Equal(s.T(), 1, refundN, "只退一次积分")
}

// TestAuditOutputDuplicateCallbackNoDoubleRefund 重复回调：审计记录终态短路 + project 终态短路 →
// 不重复落定、不重复退积分。
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

	// 第一次：fail + 退积分（种子 100 - 扣 10 + 退 10 = 110）
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))
	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus)
	testx.Equal(s.T(), 110, mustBalance(s.T(), s.deps.points, ctx, userID))

	// 第二次（同一 record）：终态短路 → 不再退款、终态不被覆盖
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))
	project, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus)
	testx.Equal(s.T(), 110, mustBalance(s.T(), s.deps.points, ctx, userID), "重复回调不重复退积分")

	// audit_record 终态不被覆盖
	rec, err := s.deps.records.Get(ctx, recordID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), riskdomain.StatusFail, rec.Status)
}

// TestAuditOutputThirdIDIsProjectID 回调仅带 projectID（无 recordID，旧协议兼容）：
// 跳过审计记录对账（recordID=0），仍通知 creation 落定 project（终态以 project 侧短路兜底），
// 退款由 project 侧幂等处理。
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

	// Extra 无 callback_record_id → recordID=0，跳过审计记录对账，仅通知 creation
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, &riskcontracts.RiskCallbackReq{
		BizID: "x", ThirdID: sessionID, UserID: userID, Passed: false, Reason: "无记录直接拒绝",
		Extra: map[string]string{},
	}))

	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus, "无 record 仍通过 project 侧落定退款")
	testx.Equal(s.T(), 110, mustBalance(s.T(), s.deps.points, ctx, userID), "种子 100 - 扣 10 + 退 10")
}

// --- 工具函数 ---

// openMySQLTestDB 切到仓库根目录读 config，打开 testdb MySQL 连接。
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

// openRedisTestDB 打开 testdb Redis，使用独立的 DB 索引（DB 5），避免污染开发缓存。
//
// Redis DB 索引约定：三个集成套件共用同一 testdb MySQL，各自使用不同的 Redis DB 索引
// 以隔离命名空间、避免跨套件污染 —— monitor=3、creation=4、risk=5（黑名单 key 等）。
// 套件间串行执行（go test -p 1）保证 DB 索引不冲突，所以 risk 用 5 而非复用前两者的索引。
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

// buildRiskDeps 装配新架构组件（对齐 internal/app/app.go 的 New，聚焦 risk 应用层）。
//
// 与 creation 集成测试的 buildCreationDeps 同构，但用"会出现的敏感词"（默认表）以
// 验证拦截路径；依赖反转：CompensationSink / AuditResultSink 由 creation.infrastructure
// 实现并注入。
func buildRiskDeps(t *testing.T, db *gorm.DB, rdb *redis.Client) *riskDeps {
	t.Helper()

	// points context
	pointsRepo := pointsinfra.NewPointsRepositoryImpl(db)
	pointsApp := pointsapp.NewPointsApp(pointsRepo)

	// risk context（本地敏感词后端）
	riskRecords := riskinfra.NewAuditRecordRepositoryImpl(db)
	auditBackend := riskinfra.NewLocalAuditBackend()

	// creation context
	projRepo := creationinfra.NewProjectRepositoryImpl(db)
	compRepo := creationinfra.NewCompensationRepositoryImpl(db)

	// 依赖反转：creation 实现 risk 侧 AuditResultSink / CompensationSink 并注入
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

// seedBalance 给用户造初始余额（flow_type=2 入账，等价旧 seedBalance）。
func seedBalance(t *testing.T, db *gorm.DB, userID int64, amount int) {
	t.Helper()
	now := time.Now().Unix()
	testx.NoError(t, db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, 'seed', ?, 2, ?, 'test seed', 1, ?)",
		userID, fmt.Sprintf("risk_seed_%d", userID), amount, now,
	).Error)
}

// seedConsumedProject 造一个已扣分的 project + 对应扣分流水（等价 create 之后的状态）。
// PointsCost 与 PointsOrderID 必须非零：扣分流水按 points_order_id 唯一，退款靠它对账。
func seedConsumedProject(t *testing.T, db *gorm.DB, projects creationdomain.ProjectRepository, ctx context.Context, p *creationdomain.Project) {
	t.Helper()
	now := time.Now().Unix()
	testx.NoError(t, db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, ?, ?, 1, ?, 'consume', 1, ?)",
		p.UserID, p.ProjectID, p.PointsOrderID, p.PointsCost, now,
	).Error)
	testx.NoError(t, projects.Insert(ctx, p))
}

// seedAuditingRecord 插一条 auditing 的审计记录（等价 AsyncAudit 提交成功后的状态）。
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

// mustAtoiID 把 extra 里的 string 型 callback_record_id 解析成 uint64。
func mustAtoiID(t *testing.T, s string) uint64 {
	t.Helper()
	id, err := strconv.ParseUint(s, 10, 64)
	testx.NoError(t, err)
	return id
}

// mustBalance 查用户余额（断言辅助）。
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
