//go:build integration

// Package monitor 阶段 5 monitor job 集成测试：走真实 MySQL。
//
// 运行方式（需 MySQL 可用）：
//
//	cd base-server && go test -tags=integration ./internal/monitor/ -v
//
// 覆盖阶段 5 的验收标准（补偿重试 + 超时兜底）：
//   - 补偿表 status=0 的记录被 RunCompensationJob 重试：async_audit 类重新提交（auditing 记录被标记已提交）
//   - 补偿重试有上限（3 次），超限放弃时退积分 + 置 fail
//   - 超过 24h 的 running 项目被 RunTimeoutJob 退积分 + 置 fail
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

// TestCompensationJobRetriesAsyncAudit 验收：补偿表 status=0 的 async_audit 记录被重试 → 重新提交。
//
// 构造：一条 status=auditing 的 risk_audit_record + 一条对应补偿记录（payload 含 content_text）。
// 跑 RunCompensationJob → RetryAsyncAudit 复用原始记录 submitAuditRequest → 记录 task_input 被写 → 补偿标记 done。
func TestCompensationJobRetriesAsyncAudit(t *testing.T) {
	db := openMySQLTestDB(t)
	truncateMonitorTables(t, db)
	m := buildMonitorDeps(t, db, nil)
	ctx := context.Background()
	userID := int64(82001)
	sessionID := "session-monitor-async-audit"

	// 建 auditing 记录（AsyncAudit 失败前的产物，分表后落 cg_risk_audit_record）
	recordID, err := m.riskAudit.Insert(ctx, &riskmodel.AuditRecord{
		UserID: userID, ProjectID: sessionID,
		TaskType: "risk:async:dujiaapp_mixvideo60s_t2v_output", Status: "auditing",
	})
	testx.NoError(t, err)

	// 建补偿记录（status=0），payload 模拟 insertAuditCompensation 写入
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

	// 跑补偿 job
	testx.NoError(t, RunCompensationJob(ctx, m.deps))

	// 补偿被标记 done
	comp, err := m.comp.GetByBizID(ctx, fmt.Sprintf("%d", recordID), creationdomain.BizTypeAsyncAudit)
	testx.NoError(t, err)
	testx.Equal(t, creationdomain.CompensationStatusDone, comp.Status, "重试成功 → 补偿标记 done")

	// auditing 记录被标记已提交（task_input 写入 sub_product + callback 纽带）
	record, err := m.riskAudit.Get(ctx, recordID)
	testx.NoError(t, err)
	testx.Equal(t, "auditing", record.Status, "记录保持 auditing（等风控回调）")
	testx.Contains(t, record.TaskInput, "sub_product", "重试后 task_input 写入提交标记")
}

// TestCompensationJobAbandonsOverLimit 验收：补偿重试超上限（3 次）→ 放弃 + 退积分 + 置 fail。
//
// 构造：一个已扣分的 running 项目 + 一条 retry_count=2 的 async_audit 补偿记录
// （payload 缺 content_text，重试必失败）。跑 job → 重试失败 → retry_count 到 3 →
// 放弃 → 退积分 + 置 fail。
func TestCompensationJobAbandonsOverLimit(t *testing.T) {
	db := openMySQLTestDB(t)
	truncateMonitorTables(t, db)
	m := buildMonitorDeps(t, db, nil)
	ctx := context.Background()
	userID := int64(82002)
	sessionID := "session-monitor-abandon"

	// 造余额 + 扣分 + 建 running 项目（已扣积分）
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

	// 补偿记录：retry_count=2，payload 缺 content_text（重试必失败）
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

	// 跑补偿 job
	testx.NoError(t, RunCompensationJob(ctx, m.deps))

	// 补偿被标记 done（放弃）
	comp, err := m.comp.GetByBizID(ctx, "pl-99002", creationdomain.BizTypeAsyncAudit)
	testx.NoError(t, err)
	testx.Equal(t, creationdomain.CompensationStatusDone, comp.Status, "超上限 → 放弃")
	testx.Equal(t, 3, comp.RetryCount, "重试次数累加到 3")

	// 兜底：项目退积分 + 置 fail
	project, err := m.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(t, err)
	testx.Equal(t, creationdomain.ProjectStatusFail, project.Status, "放弃时兜底置 fail")
	testx.Equal(t, creationdomain.PointStatusRefund, project.PointsStatus, "放弃时兜底退积分")
	testx.Contains(t, project.FailReason, "超时", "兜底走 RefundAndFailTimeout")
}

// TestTimeoutJobRefundsStuckProjects 验收：超过 24h 的 running 项目退积分 + 置 fail。
func TestTimeoutJobRefundsStuckProjects(t *testing.T) {
	db := openMySQLTestDB(t)
	truncateMonitorTables(t, db)
	m := buildMonitorDeps(t, db, nil)
	ctx := context.Background()
	userID := int64(82003)
	sessionID := "session-monitor-timeout"

	// 造余额 + 扣分 + 建 25h 前创建、仍 running 的项目
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

	// 跑超时 job
	testx.NoError(t, RunTimeoutJob(ctx, m.deps, 24))

	project, err := m.projects.GetByProjectID(ctx, sessionID)
	testx.NoError(t, err)
	testx.Equal(t, creationdomain.ProjectStatusFail, project.Status, "超时项目置 fail")
	testx.Equal(t, creationdomain.PointStatusRefund, project.PointsStatus, "超时项目退积分")

	// 余额：种子(100) + 退回(10) = 110（退款是 flow_type=2 入账，加回余额）
	balance, err := m.points.GetBalance(ctx, userID)
	testx.NoError(t, err)
	testx.Equal(t, 110, balance, "超时兜底退回积分（种子 100 + 退款 10）")
}

// TestTimeoutJobSkipsFreshProjects 验收：24h 内的 running 项目不被超时 job 处理。
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
	testx.Equal(t, creationdomain.ProjectStatusRunning, project.Status, "24h 内项目不被超时处理")
}
