//go:build integration

// Package app_test Task 6.3: end-to-end full-chain tests at the HTTP layer.
//
// How to run (requires MySQL/Redis, serial):
//
//	cd base-server && go test -tags=integration -p 1 ./internal/app/... -count=1 -v
//
// Unlike Task 6.1's application-layer integration tests (creation_app_integration_test.go calling app services directly),
// this suite **really goes through HTTP** — app.New(cfg) assembles the full routes + middleware +
// handler parameter binding, driving the whole chain with real HTTP requests per the main doc §4 mix-cut sequence:
//
//	scriptwriter pre_create(role=3) → scriptwriter async_post_create → task_detail callback(3:mix_video)
//	→ final-video pre_create(role=5) → create → final-video async_post_create
//	→ task_detail callback(5:mix_video, set audit first) → risk callback(passed) → done
//
// Verification goal: an upstream agent can really drive the full chain over HTTP, ending in done +
// points deducted exactly once with no refund.
//
// Key design (aligned with Task 6.1):
//   - The local sensitive-word backend (LocalAuditBackend, wired by default in app.New) has a default word list that does not
//     contain test content like "夏天" or "你好世界", so input/output risk control passes automatically across the chain; no backend swap needed.
//   - Callback handlers execute asynchronously in the CallbackDispatcher goroutine: use polling as the sync point,
//     and after the final-video callback you **must wait for ext.async_audit_record_id to be written** before sending the risk callback,
//     otherwise the goroutine's closing Update races with the risk callback's settling Update and overwrites the done terminal state
//     (replicating Task 6.1's sync-point ordering).
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

// CreationE2ESuite is the HTTP-layer mix-cut full-chain test suite.
type CreationE2ESuite struct {
	suite.Suite
	application *app.App
}

func (s *CreationE2ESuite) SetupSuite() {
	// Same assembly as IntegrationSuite.SetupSuite: switch to the repo root, read config → app.New
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	testx.NoError(s.T(), os.Chdir(repoRoot))

	cfg := config.Load("config/config.json")
	cfg.Redis.DB = 1 // separate db, avoiding pollution of the development cache
	application, err := app.New(cfg)
	testx.NoError(s.T(), err)
	testx.NotNil(s.T(), application.Redis)
	s.application = application
}

func (s *CreationE2ESuite) SetupTest() {
	// Clear the tables + Redis involved in the chain to avoid residue between cases
	testx.NoError(s.T(), s.application.DB.Exec("TRUNCATE TABLE cg_creation_project").Error)
	testx.NoError(s.T(), s.application.DB.Exec("TRUNCATE TABLE cg_task_callback_record").Error)
	testx.NoError(s.T(), s.application.DB.Exec("TRUNCATE TABLE cg_points_flow").Error)
	testx.NoError(s.T(), s.application.DB.Exec("TRUNCATE TABLE cg_audit_compensation").Error)
	testx.NoError(s.T(), s.application.DB.Exec("TRUNCATE TABLE cg_risk_audit_record").Error)
	testx.NoError(s.T(), s.application.Redis.FlushDB(context.Background()).Err())
}

// TestMixVideoFullChainOverHTTP drives the mix-cut full chain over real HTTP: ends in done + points deducted once with no refund.
func (s *CreationE2ESuite) TestMixVideoFullChainOverHTTP() {
	ctx := context.Background()
	userID := int64(83001)
	sessionID := "session-e2e-mixvideo-http"
	s.seedBalance(userID, 100)

	// ① Scriptwriter pre_create (role=3, input risk control passes → status=0)
	w := s.doInner(http.MethodPost, "/inner/creation/pre_create", map[string]any{
		"user_id": userID, "session_id": sessionID,
		"parent_step_id": 1001, "task_step_id": 1002,
		"project_type": "mix_video", "role_type": 3,
		"project_input": `{"input":"{\"query\":\"写一个关于夏天的故事\"}"}`,
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)
	testx.Equal(s.T(), 0, s.preCreateStatus(w), "scriptwriter input risk control passes")

	// ② Scriptwriter async_post_create: register the pipeline ID into callback_record (no project-table write)
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
	testx.Equal(s.T(), int64(0), projectsN, "the scriptwriter stage writes no project row")

	// ③ Scriptwriter task_detail callback (task_type=3:mix_video) → segment risk control passes → record=done
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

	// ④ Final-video pre_create (role=5, input risk control passes)
	w = s.doInner(http.MethodPost, "/inner/creation/pre_create", map[string]any{
		"user_id": userID, "session_id": sessionID,
		"parent_step_id": 4001, "task_step_id": 5001,
		"project_type": "mix_video", "role_type": 5,
		"project_input": `{"input":"{\"query\":\"把剧本做成视频\"}"}`,
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)
	testx.Equal(s.T(), 0, s.preCreateStatus(w), "final-video input risk control passes")

	// ⑤ create: deduct 10 points + create the project
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
	testx.Equal(s.T(), int64(10), create.Data.MemberCost, "the mix-cut unit price is 10")
	testx.Equal(s.T(), int64(90), create.Data.RemainScore, "100 - 10 = 90")

	var row e2eProjectRow
	testx.NoError(s.T(), s.application.DB.Raw(
		"SELECT status, points_status, points_order_id FROM cg_creation_project WHERE project_id = ?",
		sessionID).Scan(&row).Error)
	testx.Equal(s.T(), creationdomain.ProjectStatusRunning, row.Status)
	testx.Equal(s.T(), creationdomain.PointStatusUsed, row.PointsStatus)

	// ⑥ Final-video async_post_create: register the pipeline ID into the project
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
	testx.Equal(s.T(), "99002", prodLine, "the pipeline ID is registered")

	// ⑦ Final-video task_detail callback (task_type=5:mix_video) → set audit first + start async risk control
	w = s.doInner(http.MethodPost, "/inner/callback/task_detail", map[string]any{
		"task_id": 99002, "project_id": sessionID, "user_id": userID,
		"task_status":   "success",
		"special_info":  map[string]any{"script": "夏天的故事剧本", "description": "一段成片视频"},
		"extend_params": creationcontracts.BuildExtendParamsJSON("5:mix_video", 0, "multi", 5001, 4001),
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)

	// ⚠️ The callback handler runs asynchronously in a goroutine: waiting for the audit status is not enough — you must first wait for
	// ext.async_audit_record_id to be written (the handler's last action), otherwise the subsequent risk callback
	// races with the goroutine's closing Update and overwrites the done terminal state (replicating Task 6.1's sync-point ordering).
	asyncRecordID := s.awaitE2EAsyncAuditRecordID(ctx, sessionID)

	testx.NoError(s.T(), s.application.DB.Raw(
		"SELECT status FROM cg_creation_project WHERE project_id = ?", sessionID).Scan(&row.Status).Error)
	testx.Equal(s.T(), creationdomain.ProjectStatusAudit, row.Status, "a successful final-video output sets audit first, not done")
	testx.Greater(s.T(), asyncRecordID, int64(0), "the async risk-control recordID is passed through")

	// ⑧ risk callback passed=true → project done, no points refund
	w = s.doInner(http.MethodPost, "/inner/callback/risk", map[string]any{
		"biz_id":     "dujiaapp_mixvideo60s_t2v_output-99002",
		"third_id":   sessionID,
		"user_id":    userID,
		"passed":     true,
		"audit_type": "video",
		"extra":      map[string]string{"callback_record_id": fmt.Sprintf("%d", asyncRecordID)},
	})
	testx.Equal(s.T(), http.StatusOK, w.Code)

	// Final: done + points deducted once with no refund
	row = e2eProjectRow{}
	testx.NoError(s.T(), s.application.DB.Raw(
		"SELECT status, points_status, points_order_id FROM cg_creation_project WHERE project_id = ?",
		sessionID).Scan(&row).Error)
	testx.Equal(s.T(), creationdomain.ProjectStatusDone, row.Status, "the mix-cut chain ends in done")
	testx.Equal(s.T(), creationdomain.PointStatusUsed, row.PointsStatus, "points were not refunded")
	testx.Equal(s.T(), 90, s.mustE2EBalance(ctx, userID), "10 points deducted exactly once overall, no refund")

	// Points ledger: 1 deduction, 0 refunds
	consumeN, refundN := s.countE2EFlows(sessionID)
	testx.Equal(s.T(), 1, consumeN, "points deducted exactly once")
	testx.Equal(s.T(), 0, refundN, "no points refunded")
}

// --- Helpers ---

// e2eProjectRow is the test row structure of the project table (for multi-column queries).
type e2eProjectRow struct {
	Status        string `gorm:"column:status"`
	PointsStatus  int    `gorm:"column:points_status"`
	PointsOrderID string `gorm:"column:points_order_id"`
}

// doInner issues an internal-endpoint HTTP request (no auth required).
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

// preCreateStatus extracts data.status from the /inner/creation/pre_create response.
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

// decodeCreateResp parses the /inner/creation/create response.
func (s *CreationE2ESuite) decodeCreateResp(w *httptest.ResponseRecorder) *creationcontracts.CreateResp {
	s.T().Helper()
	var resp creationcontracts.CreateResp
	s.Require().NoError(json.Unmarshal(w.Body.Bytes(), &resp))
	return &resp
}

// seedBalance gives the user an initial balance (flow_type=2 credit, aligned with Task 6.1's seedBalance).
func (s *CreationE2ESuite) seedBalance(userID int64, amount int) {
	s.T().Helper()
	now := time.Now().Unix()
	s.Require().NoError(s.application.DB.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, 'seed', ?, 2, ?, 'test seed', 1, ?)",
		userID, fmt.Sprintf("e2e_seed_%d", userID), amount, now).Error)
}

// mustE2EBalance queries the user balance (same SQL as PointsRepositoryImpl.GetBalance).
func (s *CreationE2ESuite) mustE2EBalance(ctx context.Context, userID int64) int {
	s.T().Helper()
	var balance int
	s.Require().NoError(s.application.DB.Raw(
		"SELECT COALESCE(SUM(CASE WHEN flow_type = 2 THEN amount ELSE 0 END), 0) - "+
			"COALESCE(SUM(CASE WHEN flow_type = 1 AND status = 1 THEN amount ELSE 0 END), 0) "+
			"FROM cg_points_flow WHERE user_id = ?", userID).Scan(&balance).Error)
	return balance
}

// countE2EFlows counts the deduction/refund ledger entries of a session.
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

// awaitE2ERecordStatus polls the callback record status until target (the callback handler runs asynchronously in a goroutine).
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
	s.T().Fatalf("record %d did not reach status %s within the timeout", id, target)
}

// awaitE2EAsyncAuditRecordID polls for the async risk-control recordID write in project.ext.
//
// ⚠️ Same semantics as Task 6.1's awaitAsyncAuditRecordID: the final-video callback handler runs asynchronously
// in a goroutine; after setting audit it also starts async video risk control and passes the recordID into ext — the recordID write
// is the handler's last action. Only after it completes can the risk callback be safely simulated (otherwise the goroutine's
// closing Update races with the risk callback's settling Update).
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
	s.T().Fatalf("the async risk-control recordID of project %s was not written within the timeout", projectID)
	return 0
}

// buildSkillTaskOutput builds the task_output JSON returned by operators (aligned with Task 6.1's helper of the same name).
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

// TestCreationE2ESuite runs the mix-cut full-chain HTTP tests.
func TestCreationE2ESuite(t *testing.T) {
	suite.Run(t, new(CreationE2ESuite))
}
