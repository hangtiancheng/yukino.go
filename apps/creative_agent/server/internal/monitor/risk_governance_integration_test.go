//go:build integration

// Package monitor 风控治理集成测试：走真实 MySQL + Redis。
//
// 运行方式（需 MySQL/Redis 可用）：
//
//	cd base-server && go test -tags=integration -p 1 ./internal/monitor/ -run TestRiskStats -v
//
// 覆盖风控治理闭环（spec §8）：
//   - 同步命中 callback_record（status=fail, task_type=risk:...）→ 消费 → cg_risk_stats 累加
//   - 异步命中（先 auditing 后 Update 成 fail）→ 更新游标捕到 → 累加
//   - 命中阈值 → 评估 job → Redis 黑名单 → 送审前拦截命中
//   - 任务失败记录（task_type 非 risk: 前缀）→ 不累计（脏数据）
package monitor

import (
	"context"
	"fmt"
	"testing"
	"time"

	riskinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	riskmodel "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// riskGovernanceTruncate 清空风控治理相关数据（隔离用例）。
func riskGovernanceTruncate(t *testing.T, m *monitorDeps) {
	t.Helper()
	testx.NoError(t, m.db.Exec("TRUNCATE TABLE cg_risk_stats").Error)
	testx.NoError(t, m.db.Exec("TRUNCATE TABLE cg_task_callback_record").Error)
	testx.NoError(t, m.db.Exec("TRUNCATE TABLE cg_risk_audit_record").Error)
	testx.NoError(t, m.rdb.FlushDB(context.Background()).Err())
}

// insertRiskHit 直接插一条风控命中记录（同步命中：status=fail, task_type=risk:...，落 cg_risk_audit_record）。
func insertRiskHit(t *testing.T, m *monitorDeps, userID int64, taskType string) uint64 {
	t.Helper()
	now := time.Now().Unix()
	rec := &riskmodel.AuditRecord{
		UserID: userID, ProjectID: fmt.Sprintf("p-%d", userID),
		TaskType: taskType, Status: "fail",
		CreateTime: now, UpdateTime: now,
	}
	id, err := m.riskAudit.Insert(context.Background(), rec)
	testx.NoError(t, err)
	return id
}

// riskStatsConsumer 构建双游标消费器（内存游标，等价旧 creation.NewRiskStatsConsumer + MemoryCursor）。
func riskStatsConsumer(m *monitorDeps) *riskinfra.AuditStatsConsumer {
	return riskinfra.NewAuditStatsConsumer(
		riskinfra.NewAuditRecordPoller(m.db),
		m.riskStats,
		riskinfra.NewMemoryCursor(),
		nil,
	)
}

// TestRiskStatsConsumeAccumulates 验收：同步命中被消费 → 统计表累加。
func TestRiskStatsConsumeAccumulates(t *testing.T) {
	db := openMySQLTestDB(t)
	m := buildMonitorDeps(t, db, openRedisTestDB(t))
	riskGovernanceTruncate(t, m)
	ctx := context.Background()
	userID := int64(93001)

	insertRiskHit(t, m, userID, "risk:5:mix_video:precreate")
	insertRiskHit(t, m, userID, "risk:async:dujiaapp_mixvideo60s_t2v_output")

	// 消费一次
	consumer := riskStatsConsumer(m)
	testx.NoError(t, consumer.RunOnce(ctx))

	// 同步命中 → subProduct = 5:mix_video
	total, err := m.riskStats.SumByUserAndSubProduct(ctx, userID, "5:mix_video", 0)
	testx.NoError(t, err)
	testx.Equal(t, 1, total, "混剪 precreate 命中累加 1 次")

	// 异步命中 → subProduct = dujiaapp_mixvideo60s_t2v_output
	total2, err := m.riskStats.SumByUserAndSubProduct(ctx, userID, "dujiaapp_mixvideo60s_t2v_output", 0)
	testx.NoError(t, err)
	testx.Equal(t, 1, total2, "成片视频异步命中累加 1 次")
}

// TestRiskStatsConsumeCatchesUpdatedHit 验收：异步命中（先 auditing 后 Update 成 fail）被更新游标捕到。
func TestRiskStatsConsumeCatchesUpdatedHit(t *testing.T) {
	db := openMySQLTestDB(t)
	m := buildMonitorDeps(t, db, openRedisTestDB(t))
	riskGovernanceTruncate(t, m)
	ctx := context.Background()
	userID := int64(93002)

	// 先插 auditing 记录（异步风控提交时的中间态）
	now := time.Now().Unix()
	id, err := m.riskAudit.Insert(ctx, &riskmodel.AuditRecord{
		UserID: userID, ProjectID: "p-93002",
		TaskType: "risk:async:dujiaapp_mixvideo60s_t2v_output", Status: "auditing",
		CreateTime: now, UpdateTime: now,
	})
	testx.NoError(t, err)

	// 消费一次：auditing 不是 fail，捕不到
	consumer := riskStatsConsumer(m)
	testx.NoError(t, consumer.RunOnce(ctx))

	// 模拟风控回调 Update 成 fail（走仓储 Update，会刷新 update_time）
	testx.NoError(t, m.riskAudit.Update(ctx, id, map[string]any{"status": "fail"}))

	// 消费一次：更新游标捕到 fail 变更 → 累加
	consumer2 := riskStatsConsumer(m)
	testx.NoError(t, consumer2.RunOnce(ctx))

	total, err := m.riskStats.SumByUserAndSubProduct(ctx, userID, "dujiaapp_mixvideo60s_t2v_output", 0)
	testx.NoError(t, err)
	testx.Equal(t, 1, total, "异步风控 Update 成 fail 被更新游标累加")
}

// TestRiskStatsConsumeSkipsDirtyAndBlocksOverThreshold 验收：脏数据跳过 + 超阈值 → 黑名单 → 拦截。
func TestRiskStatsConsumeSkipsDirtyAndBlocksOverThreshold(t *testing.T) {
	db := openMySQLTestDB(t)
	m := buildMonitorDeps(t, db, openRedisTestDB(t))
	riskGovernanceTruncate(t, m)
	ctx := context.Background()
	userID := int64(93003)

	// 脏数据：task_type 非 risk: 前缀（任务失败/超时置 fail），不应累计
	insertRiskHit(t, m, userID, "5:mix_video")

	// 6 条风控命中（超过阈值 5）
	for i := 0; i < 6; i++ {
		insertRiskHit(t, m, userID, "risk:5:mix_video:precreate")
	}

	// 消费一次
	consumer := riskStatsConsumer(m)
	testx.NoError(t, consumer.RunOnce(ctx))

	// 脏数据不累计（subProduct=5:mix_video 只有脏数据那条，extractRiskSubProduct 返回空 → 不累计）
	total, err := m.riskStats.SumByUserAndSubProduct(ctx, userID, "5:mix_video", 0)
	testx.NoError(t, err)
	testx.Equal(t, 6, total, "6 条风控命中累加（脏数据那条被 extractRiskSubProduct 过滤）")

	// 跑评估 job → 命中阈值 → 黑名单写入
	testx.NoError(t, riskinfra.RunRiskOfflineJob(ctx, m.riskStats, m.rdb))
	testx.True(t, riskinfra.IsRiskBlacklisted(ctx, m.rdb, userID, "5:mix_video"), "超阈值 → 黑名单")

	// 拦截：送审前黑名单检查命中（与黑名单 key 同维度）
	testx.True(t, riskinfra.IsRiskBlacklisted(ctx, m.rdb, userID, "5:mix_video"), "送审前拦截命中")
}
