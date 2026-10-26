//go:build integration

// Package app_test Task 6.3：HTTP 层端到端完整链路测试。
//
// 运行方式（需 MySQL/Redis 可用，串行）：
//
//	cd base-server && go test -tags=integration -p 1 ./internal/app/... -count=1 -v
//
// 与 Task 6.1 的应用层集成测试（creation_app_integration_test.go 直接调 app 服务）
// 不同：本套件**真正过 HTTP**——用 app.New(cfg) 装配完整 gin 路由 + 中间件 +
// handler 参数绑定，按主文档 §四 混剪时序通过真实 HTTP 请求驱动全链路：
//
//	编剧 pre_create(role=3) → 编剧 async_post_create → task_detail 回调(3:mix_video)
//	→ 成片 pre_create(role=5) → create → 成片 async_post_create
//	→ task_detail 回调(5:mix_video，先置 audit) → risk 回调(passed) → done
//
// 验证目标：上游 agent 能真的通过 HTTP 把完整链路调起来，最终 done +
// 积分只扣一次没退。
//
// 关键设计（与 Task 6.1 对齐）：
//   - 本地敏感词后端（LocalAuditBackend，app.New 默认装配）的默认敏感词表不含
//     "夏天""你好世界"等测试内容，全链路输入/产出风控自动放行，无需替换后端。
//   - 回调 handler 在 CallbackDispatcher goroutine 中异步执行：用轮询等同步点，
//     且成片回调后**必须等到 ext.async_audit_record_id 写入**再发 risk 回调，
//     否则 goroutine 收尾 Update 会与 risk 回调的落定 Update 竞争、覆盖 done 终态
//     （复刻 Task 6.1 的同步点顺序）。
package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/app"
	creationcontracts "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	creationdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	pointsdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/domain"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx/suite"
)

// CreationE2ESuite HTTP 层混剪全链路测试套件。
type CreationE2ESuite struct {
	suite.Suite
	application *app.App
}

func (s *CreationE2ESuite) SetupSuite() {
	// 与 IntegrationSuite.SetupSuite 相同的装配：切到仓库根读 config → app.New
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	testx.NoError(s.T(), os.Chdir(repoRoot))

	cfg := config.Load("config/config.json")
	cfg.Redis.DB = 1 // 独立 db，避免污染开发缓存
	application, err := app.New(cfg)
	testx.NoError(s.T(), err)
	testx.NotNil(s.T(), application.Redis)
	s.application = application
}

func (s *CreationE2ESuite) SetupTest() {
	// 清空全链路涉及的表 + Redis，避免用例间残留
	testx.NoError(s.T(), s.application.DB.Exec("TRUNCATE TABLE cg_creation_project").Error)
	testx.NoError(s.T(), s.application.DB.Exec("TRUNCATE TABLE cg_task_callback_record").Error)
	testx.NoError(s.T(), s.application.DB.Exec("TRUNCATE TABLE cg_points_flow").Error)
	testx.NoError(s.T(), s.application.DB.Exec("TRUNCATE TABLE cg_audit_compensation").Error)
	testx.NoError(s.T(), s.application.DB.Exec("TRUNCATE TABLE cg_risk_audit_record").Error)
	testx.NoError(s.T(), s.application.Redis.FlushDB(context.Background()).Err())
}

// TestMixVideoFullChainOverHTTP 混剪全链路真实 HTTP 调用：最终 done + 积分只扣一次没退。
func (s *CreationE2ESuite) TestMixVideoFullChainOverHTTP() {
	ctx := context.Background()
	userID := int64(83001)
	sessionID := "session-e2e-mixvideo-http"
	s.seedBalance(userID, 100)

	// ① 编剧 pre_create（role=3，输入风控通过 → status=0）
	w := s.doInner(http.MethodPost, "/inner/creation/pre_create", map[string]any{
		"user_id": userID, "session_id": sessionID,
		"parent_step_id": 1001, "task_step_id": 1002,
		"project_type": "mix_video", "role_type": 3,
		"project_input": `{"input":"{\"query\":\"写一个关于夏天的故事\"}"}`,
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)
	testx.Equal(s.T(), 0, s.preCreateStatus(w), "编剧输入风控通过")

	// ② 编剧 async_post_create：登记产线 ID 到 callback_record（不写 project 表）
	w = s.doInner(http.MethodPost, "/inner/creation/async_post_create", map[string]any{
		"session_id": sessionID, "user_id": userID,
		"role_type": 3, "project_type": "mix_video",
		"task_output": s.buildSkillTaskOutput(99001, `{"script":"夏天的故事剧本"}`),
		"status":      "success",
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)

	var projectsN int64
	testx.NoError(s.T(), s.application.DB.Raw(
		"SELECT COUNT(*) FROM cg_creation_project WHERE project_id = ?", sessionID).Scan(&projectsN).Error)
	testx.Equal(s.T(), int64(0), projectsN, "编剧阶段不写 project 表")

	// ③ 编剧 task_detail 回调（task_type=3:mix_video）→ 段落风控通过 → record=done
	var scriptRecordID uint64
	testx.NoError(s.T(), s.application.DB.Raw(
		"SELECT id FROM cg_task_callback_record WHERE project_id = ? AND task_type = '3:mix_video' ORDER BY id DESC LIMIT 1",
		sessionID).Scan(&scriptRecordID).Error)
	testx.Greater(s.T(), scriptRecordID, uint64(0))

	w = s.doInner(http.MethodPost, "/inner/callback/task_detail", map[string]any{
		"task_id": 99001, "project_id": sessionID, "user_id": userID,
		"task_status":   "success",
		"special_info":  map[string]any{"script": "夏天的故事剧本"},
		"extend_params": creationcontracts.BuildExtendParamsJSON("3:mix_video", int64(scriptRecordID), "only", 1002, 1001),
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)
	s.awaitE2ERecordStatus(ctx, scriptRecordID, creationdomain.ProjectStatusDone)

	// ④ 成片 pre_create（role=5，输入风控通过）
	w = s.doInner(http.MethodPost, "/inner/creation/pre_create", map[string]any{
		"user_id": userID, "session_id": sessionID,
		"parent_step_id": 4001, "task_step_id": 5001,
		"project_type": "mix_video", "role_type": 5,
		"project_input": `{"input":"{\"query\":\"把剧本做成视频\"}"}`,
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)
	testx.Equal(s.T(), 0, s.preCreateStatus(w), "成片输入风控通过")

	// ⑤ create：扣积分 10 + 建 project
	w = s.doInner(http.MethodPost, "/inner/creation/create", map[string]any{
		"user_id": userID, "session_id": sessionID,
		"project_type":  "mix_video",
		"project_input": map[string]any{"query": "把剧本做成视频"},
		"title":         "我的混剪", "project_from": "dujia_agent",
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)
	create := s.decodeCreateResp(w)
	testx.NotNil(s.T(), create.Data)
	testx.Equal(s.T(), "success", create.Data.Status)
	testx.Equal(s.T(), int64(10), create.Data.MemberCost, "混剪单价 10")
	testx.Equal(s.T(), int64(90), create.Data.RemainScore, "100 - 10 = 90")

	var row e2eProjectRow
	testx.NoError(s.T(), s.application.DB.Raw(
		"SELECT status, points_status, points_order_id FROM cg_creation_project WHERE project_id = ?",
		sessionID).Scan(&row).Error)
	testx.Equal(s.T(), creationdomain.ProjectStatusRunning, row.Status)
	testx.Equal(s.T(), creationdomain.PointStatusUsed, row.PointsStatus)

	// ⑥ 成片 async_post_create：登记产线 ID 到 project
	w = s.doInner(http.MethodPost, "/inner/creation/async_post_create", map[string]any{
		"session_id": sessionID, "user_id": userID,
		"role_type": 5, "project_type": "mix_video",
		"task_output": s.buildSkillTaskOutput(99002, ""),
		"status":      "success",
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)

	var prodLine string
	testx.NoError(s.T(), s.application.DB.Raw(
		"SELECT production_line_id FROM cg_creation_project WHERE project_id = ?", sessionID).Scan(&prodLine).Error)
	testx.Equal(s.T(), "99002", prodLine, "产线 ID 已登记")

	// ⑦ 成片 task_detail 回调（task_type=5:mix_video）→ 先置 audit + 发起异步风控
	w = s.doInner(http.MethodPost, "/inner/callback/task_detail", map[string]any{
		"task_id": 99002, "project_id": sessionID, "user_id": userID,
		"task_status":   "success",
		"special_info":  map[string]any{"script": "夏天的故事剧本", "description": "一段成片视频"},
		"extend_params": creationcontracts.BuildExtendParamsJSON("5:mix_video", 0, "multi", 5001, 4001),
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)

	// ⚠️ 回调 handler 在 goroutine 异步执行：等 audit 状态还不够——必须先等到
	// ext.async_audit_record_id 写入（handler 的最后一个动作），否则后续 risk 回调
	// 会与 goroutine 收尾 Update 竞争、覆盖 done 终态（复刻 Task 6.1 同步点顺序）。
	asyncRecordID := s.awaitE2EAsyncAuditRecordID(ctx, sessionID)

	testx.NoError(s.T(), s.application.DB.Raw(
		"SELECT status FROM cg_creation_project WHERE project_id = ?", sessionID).Scan(&row.Status).Error)
	testx.Equal(s.T(), creationdomain.ProjectStatusAudit, row.Status, "成片产出成功先置 audit 而非 done")
	testx.Greater(s.T(), asyncRecordID, int64(0), "异步风控 recordID 已透传")

	// ⑧ risk 回调 passed=true → project done，不退积分
	w = s.doInner(http.MethodPost, "/inner/callback/risk", map[string]any{
		"biz_id":     "dujiaapp_mixvideo60s_t2v_output-99002",
		"third_id":   sessionID,
		"user_id":    userID,
		"passed":     true,
		"audit_type": "video",
		"extra":      map[string]string{"callback_record_id": fmt.Sprintf("%d", asyncRecordID)},
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)

	// 最终：done + 积分只扣一次没退
	row = e2eProjectRow{}
	testx.NoError(s.T(), s.application.DB.Raw(
		"SELECT status, points_status, points_order_id FROM cg_creation_project WHERE project_id = ?",
		sessionID).Scan(&row).Error)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, row.Status, "混剪链路终态 done")
	testx.Equal(s.T(), creationdomain.PointStatusUsed, row.PointsStatus, "积分未退")
	testx.Equal(s.T(), 90, s.mustE2EBalance(ctx, userID), "全程只扣一次 10 分，没退")

	// 积分流水：1 条扣分、0 条退款
	consumeN, refundN := s.countE2EFlows(sessionID)
	testx.Equal(s.T(), 1, consumeN, "只扣一次积分")
	testx.Equal(s.T(), 0, refundN, "未退积分")
}

// --- 工具函数 ---

// e2eProjectRow 测试用 project 表行结构（多列查询用）。
type e2eProjectRow struct {
	Status        string `gorm:"column:status"`
	PointsStatus  int    `gorm:"column:points_status"`
	PointsOrderID string `gorm:"column:points_order_id"`
}

// doInner 发起内部接口 HTTP 请求（无需鉴权）。
func (s *CreationE2ESuite) doInner(method, path string, body map[string]any) *httptest.ResponseRecorder {
	s.T().Helper()
	buf := bytes.NewBuffer(nil)
	if body != nil {
		b, err := json.Marshal(body)
		s.Require().NoError(err)
		buf = bytes.NewBuffer(b)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.application.TestHandler().ServeHTTP(w, req)
	return w
}

// preCreateStatus 从 /inner/creation/pre_create 响应提取 data.status。
func (s *CreationE2ESuite) preCreateStatus(w *httptest.ResponseRecorder) int {
	s.T().Helper()
	var resp struct {
		Errno int `json:"errno"`
		Data  *struct {
			Status int `json:"status"`
		} `json:"data"`
	}
	s.Require().NoError(json.Unmarshal(w.Body.Bytes(), &resp))
	s.Require().NotNil(resp.Data)
	return resp.Data.Status
}

// decodeCreateResp 解析 /inner/creation/create 响应。
func (s *CreationE2ESuite) decodeCreateResp(w *httptest.ResponseRecorder) *creationcontracts.CreateResp {
	s.T().Helper()
	var resp creationcontracts.CreateResp
	s.Require().NoError(json.Unmarshal(w.Body.Bytes(), &resp))
	return &resp
}

// seedBalance 给用户造初始余额（flow_type=2 入账，对齐 Task 6.1 的 seedBalance）。
func (s *CreationE2ESuite) seedBalance(userID int64, amount int) {
	s.T().Helper()
	now := time.Now().Unix()
	s.Require().NoError(s.application.DB.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, 'seed', ?, 2, ?, 'test seed', 1, ?)",
		userID, fmt.Sprintf("e2e_seed_%d", userID), amount, now).Error)
}

// mustE2EBalance 查用户余额（与 PointsRepositoryImpl.GetBalance 同 SQL）。
func (s *CreationE2ESuite) mustE2EBalance(ctx context.Context, userID int64) int {
	s.T().Helper()
	var balance int
	s.Require().NoError(s.application.DB.Raw(
		"SELECT COALESCE(SUM(CASE WHEN flow_type = 2 THEN amount ELSE 0 END), 0) - "+
			"COALESCE(SUM(CASE WHEN flow_type = 1 AND status = 1 THEN amount ELSE 0 END), 0) "+
			"FROM cg_points_flow WHERE user_id = ?", userID).Scan(&balance).Error)
	return balance
}

// countE2EFlows 统计某 session 的扣分/退款流水条数。
func (s *CreationE2ESuite) countE2EFlows(projectID string) (consumeN, refundN int) {
	s.T().Helper()
	var rows []struct {
		FlowType int `json:"flow_type"`
	}
	s.Require().NoError(s.application.DB.Raw(
		"SELECT flow_type FROM cg_points_flow WHERE project_id = ?", projectID).Scan(&rows).Error)
	for _, r := range rows {
		if r.FlowType == pointsdomain.FlowTypeConsume {
			consumeN++
		} else if r.FlowType == pointsdomain.FlowTypeRefund {
			refundN++
		}
	}
	return consumeN, refundN
}

// awaitE2ERecordStatus 轮询回调记录状态直到 target（回调 handler 在 goroutine 异步执行）。
func (s *CreationE2ESuite) awaitE2ERecordStatus(ctx context.Context, id uint64, target string) {
	s.T().Helper()
	for i := 0; i < 100; i++ {
		var status string
		err := s.application.DB.Raw("SELECT status FROM cg_task_callback_record WHERE id = ?", id).Scan(&status).Error
		s.Require().NoError(err)
		if status == target {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.T().Fatalf("record %d 未在超时内达到状态 %s", id, target)
}

// awaitE2EAsyncAuditRecordID 轮询 project.ext 里的异步风控 recordID 写入。
//
// ⚠️ 与 Task 6.1 的 awaitAsyncAuditRecordID 同语义：成片回调 handler 在 goroutine
// 异步执行，置 audit 后还要发起异步视频风控并把 recordID 透传到 ext——recordID 写入
// 是 handler 的最后一个动作。只有等它完成，才能安全地模拟 risk 回调（否则 goroutine
// 收尾 Update 会与 risk 回调的落定 Update 竞争）。
func (s *CreationE2ESuite) awaitE2EAsyncAuditRecordID(ctx context.Context, projectID string) int64 {
	s.T().Helper()
	for i := 0; i < 100; i++ {
		var ext string
		err := s.application.DB.Raw("SELECT ext FROM cg_creation_project WHERE project_id = ?", projectID).Scan(&ext).Error
		s.Require().NoError(err)
		parsed, err := creationdomain.ParseExt(ext)
		s.Require().NoError(err)
		if parsed.AsyncAuditRecordID > 0 {
			return parsed.AsyncAuditRecordID
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.T().Fatalf("project %s 的异步风控 recordID 未在超时内写入", projectID)
	return 0
}

// buildSkillTaskOutput 构造算子返回的 task_output JSON（对齐 Task 6.1 的同名 helper）。
func (s *CreationE2ESuite) buildSkillTaskOutput(taskID int64, specialInfo string) string {
	b, _ := json.Marshal(map[string]any{
		"errno": 0,
		"data": map[string]any{
			"task_id":      taskID,
			"task_status":  "success",
			"special_info": specialInfo,
			"fail_reason":  "",
		},
	})
	return string(b)
}

// TestCreationE2ESuite 运行混剪全链路 HTTP 测试。
func TestCreationE2ESuite(t *testing.T) {
	suite.Run(t, new(CreationE2ESuite))
}
