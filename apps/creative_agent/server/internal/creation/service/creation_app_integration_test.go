//go:build integration

// Package application_test creation 应用服务集成测试：走真实 MySQL + Redis。
//
// 运行方式（需 MySQL/Redis 可用）：
//
//	cd base-server && go test -tags=integration ./internal/creation/... -count=1 -v
//
// 覆盖 Task 6.1 的 5 条核心链路（从旧 phase4/phase5 集成测试行为迁移，
// 改用新 DDD 架构组件装配）：
//   - 妙图同步链路：pre_create → create → sync_post_create → done，积分只扣一次
//   - 混剪完整链路：编剧 pre_create → async_post_create → 编剧回调 → 成片 pre_create →
//     create → async_post_create → 成片回调 → audit 中间态 → 风控回调 passed → done，积分只扣一次没退
//   - 终态短路：done/fail 后重复回调不处理、终态不被覆盖
//   - 幂等：重复 create 不重复扣分；重复风控回调不重复退积分
//   - 超时兜底：running 超时 → 退积分 + fail（RefundAndFailTimeout）
//
// 装配（对齐 internal/app/app.go 的 New）：用本地敏感词后端（LocalAuditBackend，
// 敏感词表替换为"不会出现的词"以放行全链路内容）+ 真实 points/risk/creation 仓储 +
// Redis 黑名单检查器。回调分发走 CallbackDispatcher（goroutine 异步执行，测试用轮询等待终态）。
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

// creationDeps 是测试装配好的新架构组件（等价 internal/app/app.go 的 New 产物）。
type creationDeps struct {
	db          *gorm.DB
	rdb         *redis.Client
	creationApp *creationapp.CreationApp // 四个用例门面
	dispatcher  *creationinfra.CallbackDispatcher
	auditOutput *riskapp.AuditOutputApp // 风控回调终态落定
	projects    creationdomain.ProjectRepository
	records     creationdomain.CallbackRecordRepository
	riskAudit   riskdomain.AuditRecordRepository
	points      pointsapp.PointsApp
}

// CreationAppIntegrationSuite 集成测试套件。
type CreationAppIntegrationSuite struct {
	suite.Suite
	deps *creationDeps
}

func (s *CreationAppIntegrationSuite) SetupSuite() {
	t := s.T()
	db := mustOpenMySQL(t)
	rdb := mustOpenRedis(t)

	// 各 bounded context 表结构 AutoMigrate（幂等，缺列才补）
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

// TestMiaotuSyncChain 妙图同步链路：pre_create → create → sync_post_create → done，积分只扣一次。
func (s *CreationAppIntegrationSuite) TestMiaotuSyncChain() {
	ctx := context.Background()
	userID := int64(810001)
	sessionID := "session-integration-miaotu-sync"
	seedBalance(s.T(), s.deps.db, userID, 100)

	// 1. pre_create（role=4 视觉设计师 / 妙图）——输入风控通过
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
	testx.Equal(s.T(), 0, pre.Data.Status, "妙图输入风控通过")

	// 2. create 扣积分（妙图单价 5）
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

	// 3. sync_post_create：一步到终态 done（图片风控 → 文本风控 → 全部通过）
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
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, projects[0].Status, "妙图同步一步到终态")
	testx.Equal(s.T(), project.ID, projects[0].ID, "全程只有一个 project")

	// 积分只扣一次（100 - 5 = 95），未退
	testx.Equal(s.T(), 95, s.mustBalance(ctx, userID), "妙图扣 5 分")
	flows, err := s.deps.points.ListByProject(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Len(s.T(), flows, 1, "只有一条积分流水")
	testx.Equal(s.T(), pointsdomain.FlowTypeConsume, flows[0].FlowType, "是扣分流水")
}

// TestMixVideoFullChain 混剪完整链路：编剧 → 成片 → audit 中间态 → 风控回调通过 → done，
// 积分只扣一次没退；含幂等与短路断言。
func (s *CreationAppIntegrationSuite) TestMixVideoFullChain() {
	ctx := context.Background()
	userID := int64(810002)
	sessionID := "session-integration-mixvideo-full"
	seedBalance(s.T(), s.deps.db, userID, 100)

	// 1. 编剧 pre_create（role=3，输入风控通过）
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
	testx.Equal(s.T(), 0, screenplayPre.Data.Status, "编剧输入风控通过")

	// 2. 编剧 async_post_create：登记产线 ID 到 callback_record（不写 project 表）
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
	testx.Len(s.T(), projects, 0, "编剧阶段不写 project 表")

	scriptRecord, err := s.deps.records.GetByProjectAndType(ctx, sessionID, "3:mix_video")
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusRunning, scriptRecord.Status)

	// 3. 编剧回调（task_detail，task_type=3:mix_video）→ 段落风控通过 → record=done
	s.deps.dispatcher.Dispatch(ctx, &creationcontracts.CallbackReq{
		TaskID:       99001,
		ProjectID:    sessionID,
		UserID:       userID,
		TaskStatus:   creationdomain.CallbackTaskStatusSuccess,
		SpecialInfo:  json.RawMessage(`{"script":"夏天的故事剧本"}`),
		ExtendParams: creationcontracts.BuildExtendParamsJSON("3:mix_video", int64(scriptRecord.ID), "only", 1002, 1001),
	})
	s.awaitRecordStatus(ctx, scriptRecord.ID, creationdomain.ProjectStatusDone)

	// 4. 成片 pre_create（role=5，输入风控通过）
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
	testx.Equal(s.T(), 0, editorPre.Data.Status, "成片输入风控通过")

	// 5. 成片 create（扣积分 10 + 建 project）
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
	testx.Len(s.T(), projects, 1, "全程只有一个 project")
	project := projects[0]
	pointsOrderID := project.PointsOrderID
	testx.Equal(s.T(), creationdomain.ProjectStatusRunning, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusUsed, project.PointsStatus)
	testx.Equal(s.T(), 10, project.PointsCost, "混剪单价 10")

	// 6. 成片 async_post_create：登记产线 ID 到 project
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
	testx.Equal(s.T(), "99002", projects[0].ProductionLineID, "产线 ID 已登记")

	// 7. 成片回调（task_detail，task_type=5:mix_video）→ 先置 audit（不是 done！）
	s.deps.dispatcher.Dispatch(ctx, &creationcontracts.CallbackReq{
		TaskID:       99002,
		ProjectID:    sessionID,
		UserID:       userID,
		TaskStatus:   creationdomain.CallbackTaskStatusSuccess,
		SpecialInfo:  json.RawMessage(`{"script":"夏天的故事剧本","description":"一段成片视频"}`),
		ExtendParams: creationcontracts.BuildExtendParamsJSON("5:mix_video", 0, "multi", 5001, 4001),
	})
	// ⚠️ 回调 handler 在 goroutine 异步执行：等 audit 状态还不够——
	// 必须先等到 ext.async_audit_record_id 写入（handler 的最后一个动作），
	// 否则后续风控回调会与 goroutine 的收尾 Update 竞争、覆盖 done 终态。
	asyncRecordID := s.awaitAsyncAuditRecordID(ctx, sessionID)

	p, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusAudit, p.Status, "成片产出成功先置 audit 而非 done")
	testx.Greater(s.T(), asyncRecordID, int64(0), "异步风控 recordID 已透传")

	// 8. 风控回调（passed=true）→ audit 记录落 done + project 置 done，不退积分
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
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, p.Status, "风控通过 → done")
	testx.Equal(s.T(), creationdomain.PointStatusUsed, p.PointsStatus, "未退款")

	// 积分只扣一次（100 - 10 = 90），全程无退款
	testx.Equal(s.T(), 90, s.mustBalance(ctx, userID), "全程只扣一次 10 分，没退")
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
	testx.Equal(s.T(), 1, consumeN, "只扣一次积分")
	testx.Equal(s.T(), 0, refundN, "未退积分")

	// 9. 重复 create（幂等）：session 已有 project → 返回 success，不重复扣分
	create2, err := s.deps.creationApp.Create.HandleCreate(ctx, &creationcontracts.CreateReq{
		UserID:       userID,
		SessionID:    sessionID,
		ProjectType:  creationdomain.ProjectTypeMixVideo,
		ProjectInput: map[string]any{"query": "把剧本做成视频"},
	})
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), "success", create2.Data.Status, "重复 create 幂等返回成功")
	testx.Equal(s.T(), 90, s.mustBalance(ctx, userID), "重复 create 不重复扣分")
	projects, err = s.deps.projects.GetBySessionID(ctx, sessionID, userID)
	testx.NoError(s.T(), err)
	testx.Len(s.T(), projects, 1, "重复 create 不新建 project")

	// 10. 重复风控回调（同一 audit record）：终态短路 → 不退款、终态不被覆盖
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))
	p, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, p.Status, "终态不被覆盖")
	testx.Equal(s.T(), creationdomain.PointStatusUsed, p.PointsStatus, "重复风控回调不重复退积分")
	testx.Equal(s.T(), 90, s.mustBalance(ctx, userID), "重复风控回调不退款")
	testx.Equal(s.T(), pointsOrderID, p.PointsOrderID, "points_order_id 不变")
}

// TestCreateIdempotentNoDoubleDeduct 幂等：重复 create 同一 session 不重复扣分。
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
	testx.Equal(s.T(), 95, s.mustBalance(ctx, userID), "首扣 5 分")

	// 重复 create：session 下已有 project → 幂等返回 success，不再扣分
	second, err := s.deps.creationApp.Create.HandleCreate(ctx, req)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), "success", second.Data.Status)
	testx.Equal(s.T(), 95, s.mustBalance(ctx, userID), "重复 create 不重复扣分")

	projects, err := s.deps.projects.GetBySessionID(ctx, sessionID, userID)
	testx.NoError(s.T(), err)
	testx.Len(s.T(), projects, 1, "重复 create 不新建 project")
}

// TestRepeatedRiskCallbackNoDoubleRefund 幂等：同一审计记录重复风控回调只退一次积分。
func (s *CreationAppIntegrationSuite) TestRepeatedRiskCallbackNoDoubleRefund() {
	ctx := context.Background()
	userID := int64(810004)
	sessionID := "session-integration-risk-repeat"
	seedBalance(s.T(), s.deps.db, userID, 100)

	// 建已扣分的 project（create 扣 10 分，余额 90），模拟成片回调已置 audit
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

	// 插一条 auditing 的审计记录（AsyncAudit 的产物）
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

	// 第一次：退积分 + fail
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))
	project, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus)
	testx.Equal(s.T(), 110, s.mustBalance(ctx, userID), "退回混剪 10 分（种子 100 + 退款 10）")

	// 第二次：审计记录终态短路 + project 终态短路 → 不再退款
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, riskCb))
	testx.Equal(s.T(), 110, s.mustBalance(ctx, userID), "重复风控回调不重复退积分")
}

// TestCallbackAfterTerminalNoOp 终态短路：done/fail 后重复回调不处理、终态不被覆盖。
func (s *CreationAppIntegrationSuite) TestCallbackAfterTerminalNoOp() {
	ctx := context.Background()
	userID := int64(810005)
	sessionID := "session-integration-cb-terminal"
	seedBalance(s.T(), s.deps.db, userID, 100)

	// 建已 done 的 project（积分已扣未退）
	testx.NoError(s.T(), s.deps.projects.Insert(ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusDone,
		PointsStatus: creationdomain.PointStatusUsed, PointsCost: 10,
		PointsOrderID: sessionID + "_1", RetryNum: 1,
	}))

	// done 后重复成片回调（success）→ handler 终态短路，状态不变
	s.deps.dispatcher.Dispatch(ctx, &creationcontracts.CallbackReq{
		TaskID:       99001,
		ProjectID:    sessionID,
		UserID:       userID,
		TaskStatus:   creationdomain.CallbackTaskStatusSuccess,
		SpecialInfo:  json.RawMessage(`{"script":"剧本","description":"视频"}`),
		ExtendParams: creationcontracts.BuildExtendParamsJSON("5:mix_video", 0, "multi", 5001, 4001),
	})
	// 回调在 goroutine 异步执行，短等让 handler 跑完（无状态变更，轮询即可）
	s.awaitProjectStatus(ctx, sessionID, creationdomain.ProjectStatusDone)

	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, project.Status, "终态不被回调覆盖")
	testx.Equal(s.T(), creationdomain.PointStatusUsed, project.PointsStatus, "终态后不重复退积分")

	// done 后风控回调 !passed → 终态短路，不退款、不覆盖
	testx.NoError(s.T(), s.deps.auditOutput.Handle(ctx, &riskcontracts.RiskCallbackReq{
		BizID: "x", ThirdID: sessionID, UserID: userID, Passed: false, Reason: "违规",
	}))
	project, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, project.Status)
	testx.Equal(s.T(), creationdomain.PointStatusUsed, project.PointsStatus)
	testx.Equal(s.T(), 100, s.mustBalance(ctx, userID), "种子 100 一直没动（未扣未退）")
}

// TestRefundAndFailTimeout 超时兜底：running 超时 → 退积分 + fail；已 fail 再次兜底 no-op。
func (s *CreationAppIntegrationSuite) TestRefundAndFailTimeout() {
	ctx := context.Background()
	userID := int64(810006)
	sessionID := "session-integration-timeout"
	seedBalance(s.T(), s.deps.db, userID, 100)
	now := time.Now().Unix()

	// 造扣分流水（模拟 create 已扣 10 分）
	testx.NoError(s.T(), s.deps.db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, ?, ?, 1, 10, 'consume', 1, ?)",
		userID, sessionID, sessionID+"_1", now,
	).Error)

	// 造一个超时窗口前创建、仍 running 且已扣分的 project
	stuckTime := time.Now().Add(-25 * time.Hour).Unix()
	testx.NoError(s.T(), s.deps.projects.Insert(ctx, &creationdomain.Project{
		UserID: userID, SessionID: sessionID, ProjectID: sessionID,
		ProjectType: creationdomain.ProjectTypeMixVideo, Status: creationdomain.ProjectStatusRunning,
		PointsStatus: creationdomain.PointStatusUsed, PointsCost: 10,
		PointsOrderID: sessionID + "_1", RetryNum: 1,
		CreateTime: stuckTime, UpdateTime: stuckTime,
	}))

	// 超时兜底：退积分 + fail
	project, err := s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.NoError(s.T(), s.deps.creationApp.PostCreate.RefundAndFailTimeout(ctx, project))

	project, err = s.deps.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(s.T(), err)
	testx.Equal(s.T(), creationdomain.ProjectStatusFail, project.Status, "超时置 fail")
	testx.Equal(s.T(), creationdomain.PointStatusRefund, project.PointsStatus, "超时退积分")
	testx.Equal(s.T(), 110, s.mustBalance(ctx, userID), "种子 100 + 退回 10 = 110")

	// 幂等：已 fail 的项目再次超时兜底 → no-op，不重复退款
	testx.NoError(s.T(), s.deps.creationApp.PostCreate.RefundAndFailTimeout(ctx, project))
	testx.Equal(s.T(), 110, s.mustBalance(ctx, userID), "重复超时兜底不重复退款")
}

// --- 工具函数 ---

// mustBalance 查用户余额（断言辅助）。
func (s *CreationAppIntegrationSuite) mustBalance(ctx context.Context, userID int64) int {
	s.T().Helper()
	b, err := s.deps.points.GetBalance(ctx, userID)
	testx.NoError(s.T(), err)
	return b
}

// awaitRecordStatus 轮询回调记录状态直到 target（回调 handler 在 goroutine 异步执行）。
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
	s.T().Fatalf("record %d 未在超时内达到状态 %s", id, target)
}

// awaitProjectStatus 轮询作品状态直到 target。
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
	s.T().Fatalf("project %s 未在超时内达到状态 %s", projectID, target)
}

// awaitAsyncAuditRecordID 轮询 project.ext 里的异步风控 recordID 写入。
//
// ⚠️ 成片回调 handler 在 goroutine 异步执行，置 audit 后还要发起异步视频风控并
// 把 recordID 透传到 ext——recordID 写入是 handler 的最后一个动作。只有等它完成，
// 才能安全地模拟风控回调（否则 goroutine 收尾 Update 会与风控回调的落定 Update 竞争）。
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
	s.T().Fatalf("project %s 的异步风控 recordID 未在超时内写入", projectID)
	return 0
}

// mustOpenMySQL 切到仓库根目录读 config，打开 testdb MySQL 连接。
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

// mustOpenRedis 打开 testdb Redis（独立 DB，避免污染开发缓存）。
func mustOpenRedis(t *testing.T) *redis.Client {
	t.Helper()
	cfg := config.Load("config/config.json")
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr(), Password: cfg.Redis.Password, DB: 4})
	testx.NoError(t, rdb.Ping(context.Background()).Err())
	return rdb
}

// buildCreationDeps 装配新架构组件（对齐 internal/app/app.go 的 New，但用本地敏感词后端）。
//
// ⚠️ 关键：LocalAuditBackend.Submit 在敏感词表为空时会回退到默认敏感词表
// （DefaultSensitiveWords），所以这里用"不会出现的词"而非空表，让全链路内容通过。
func buildCreationDeps(t *testing.T, db *gorm.DB, rdb *redis.Client) *creationDeps {
	t.Helper()

	// points context
	pointsRepo := pointsinfra.NewPointsRepositoryImpl(db)
	pointsApp := pointsapp.NewPointsApp(pointsRepo)

	// risk context（本地敏感词后端，放行内容）
	riskRecords := riskinfra.NewAuditRecordRepositoryImpl(db)
	auditBackend := riskinfra.NewLocalAuditBackend()
	auditBackend.SetSensitiveWords([]string{"__no_such_sensitive_word__"})

	// creation context
	projRepo := creationinfra.NewProjectRepositoryImpl(db)
	cbRepo := creationinfra.NewCallbackRecordRepositoryImpl(db)
	compRepo := creationinfra.NewCompensationRepositoryImpl(db)

	// 依赖反转：creation 实现 risk 侧 AuditResultSink / CompensationSink 并注入
	compSink := creationinfra.NewCompensationSinkImpl(compRepo)
	onAuditResult := creationinfra.NewOnAuditResultImpl(projRepo, pointsApp)
	blacklistChecker := riskinfra.NewRedisBlacklistChecker(rdb)
	riskAuditApp := riskapp.NewRiskAuditApp(auditBackend, riskRecords, compSink, blacklistChecker)
	auditOutputApp := riskapp.NewAuditOutputApp(riskRecords, onAuditResult)

	// creation 应用门面 + 回调分发器
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

// seedBalance 给用户造初始余额（flow_type=2 入账，等价旧 seedBalance）。
func seedBalance(t *testing.T, db *gorm.DB, userID int64, amount int) {
	t.Helper()
	now := time.Now().Unix()
	testx.NoError(t, db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, 'seed', ?, 2, ?, 'test seed', 1, ?)",
		userID, fmt.Sprintf("creation_seed_%d", userID), amount, now,
	).Error)
}

// buildSkillTaskOutput 构造算子返回的 task_output JSON。
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
