//go:build integration

// Package monitor risk-governance integration tests: against real MySQL + Redis.
//
// How to run (requires MySQL/Redis):
//
//	cd base-server && go test -tags=integration -p 1 ./internal/monitor/ -run TestRiskStats -v
//
// Covers the risk-governance loop (spec §8):
//   - Sync hits in callback_record (status=fail, task_type=risk:...) → consumed → cg_risk_stats accumulates
//   - Async hits (auditing first, later Updated to fail) → caught by the update cursor → accumulated
//   - Threshold reached → evaluation job → Redis blacklist → intercepted before submission
//   - Task-failure records (task_type without the risk: prefix) → not counted (dirty data)
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

// riskGovernanceTruncate clears risk-governance data (case isolation).
func riskGovernanceTruncate(t *testing.T, m *monitorDeps) {
	t.Helper()
	testx.NoError(t, m.db.Exec("TRUNCATE TABLE cg_risk_stats").Error)
	testx.NoError(t, m.db.Exec("TRUNCATE TABLE cg_task_callback_record").Error)
	testx.NoError(t, m.db.Exec("TRUNCATE TABLE cg_risk_audit_record").Error)
	testx.NoError(t, m.rdb.FlushDB(context.Background()).Err())
}

// insertRiskHit inserts a risk-hit record directly (sync hit: status=fail, task_type=risk:..., stored in cg_risk_audit_record).
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

// riskStatsConsumer builds the dual-cursor consumer (in-memory cursors, equivalent to the old creation.NewRiskStatsConsumer + MemoryCursor).
func riskStatsConsumer(m *monitorDeps) *riskinfra.AuditStatsConsumer {
	return riskinfra.NewAuditStatsConsumer(
		riskinfra.NewAuditRecordPoller(m.db),
		m.riskStats,
		riskinfra.NewMemoryCursor(),
		nil,
	)
}

// TestRiskStatsConsumeAccumulates acceptance: sync hits are consumed → the stats table accumulates.
func TestRiskStatsConsumeAccumulates(t *testing.T) {
	db := openMySQLTestDB(t)
	m := buildMonitorDeps(t, db, openRedisTestDB(t))
	riskGovernanceTruncate(t, m)
	ctx := context.Background()
	userID := int64(93001)

	insertRiskHit(t, m, userID, "risk:5:mix_video:precreate")
	insertRiskHit(t, m, userID, "risk:async:dujiaapp_mixvideo60s_t2v_output")

	// Consume once
	consumer := riskStatsConsumer(m)
	testx.NoError(t, consumer.RunOnce(ctx))

	// Sync hit → subProduct = 5:mix_video
	total, err := m.riskStats.SumByUserAndSubProduct(ctx, userID, "5:mix_video", 0)
	testx.NoError(t, err)
	testx.Equal(t, 1, total, "the mix-cut precreate hit accumulates once")

	// Async hit → subProduct = dujiaapp_mixvideo60s_t2v_output
	total2, err := m.riskStats.SumByUserAndSubProduct(ctx, userID, "dujiaapp_mixvideo60s_t2v_output", 0)
	testx.NoError(t, err)
	testx.Equal(t, 1, total2, "the final-video async hit accumulates once")
}

// TestRiskStatsConsumeCatchesUpdatedHit acceptance: async hits (auditing first, later Updated to fail) are caught by the update cursor.
func TestRiskStatsConsumeCatchesUpdatedHit(t *testing.T) {
	db := openMySQLTestDB(t)
	m := buildMonitorDeps(t, db, openRedisTestDB(t))
	riskGovernanceTruncate(t, m)
	ctx := context.Background()
	userID := int64(93002)

	// Insert the auditing record first (the intermediate state of async risk submission)
	now := time.Now().Unix()
	id, err := m.riskAudit.Insert(ctx, &riskmodel.AuditRecord{
		UserID: userID, ProjectID: "p-93002",
		TaskType: "risk:async:dujiaapp_mixvideo60s_t2v_output", Status: "auditing",
		CreateTime: now, UpdateTime: now,
	})
	testx.NoError(t, err)

	// Consume once: auditing is not fail, so nothing is caught
	consumer := riskStatsConsumer(m)
	testx.NoError(t, consumer.RunOnce(ctx))

	// Simulate the risk callback Updating to fail (via the repository Update, refreshing update_time)
	testx.NoError(t, m.riskAudit.Update(ctx, id, map[string]any{"status": "fail"}))

	// Consume once: the update cursor catches the fail change → accumulate
	consumer2 := riskStatsConsumer(m)
	testx.NoError(t, consumer2.RunOnce(ctx))

	total, err := m.riskStats.SumByUserAndSubProduct(ctx, userID, "dujiaapp_mixvideo60s_t2v_output", 0)
	testx.NoError(t, err)
	testx.Equal(t, 1, total, "the async risk Update to fail is accumulated by the update cursor")
}

// TestRiskStatsConsumeSkipsDirtyAndBlocksOverThreshold acceptance: dirty data skipped + over threshold → blacklist → interception.
func TestRiskStatsConsumeSkipsDirtyAndBlocksOverThreshold(t *testing.T) {
	db := openMySQLTestDB(t)
	m := buildMonitorDeps(t, db, openRedisTestDB(t))
	riskGovernanceTruncate(t, m)
	ctx := context.Background()
	userID := int64(93003)

	// Dirty data: task_type without the risk: prefix (task failure/timeout set fail); must not accumulate
	insertRiskHit(t, m, userID, "5:mix_video")

	// 6 risk hits (over the threshold of 5)
	for i := 0; i < 6; i++ {
		insertRiskHit(t, m, userID, "risk:5:mix_video:precreate")
	}

	// Consume once
	consumer := riskStatsConsumer(m)
	testx.NoError(t, consumer.RunOnce(ctx))

	// Dirty data does not accumulate (subProduct=5:mix_video has only the dirty record; extractRiskSubProduct returns empty → no accumulation)
	total, err := m.riskStats.SumByUserAndSubProduct(ctx, userID, "5:mix_video", 0)
	testx.NoError(t, err)
	testx.Equal(t, 6, total, "6 risk hits accumulate (the dirty one is filtered by extractRiskSubProduct)")

	// Run the evaluation job → threshold hit → blacklist written
	testx.NoError(t, riskinfra.RunRiskOfflineJob(ctx, m.riskStats, m.rdb))
	testx.True(t, riskinfra.IsRiskBlacklisted(ctx, m.rdb, userID, "5:mix_video"), "over threshold → blacklisted")

	// Interception: the pre-submission blacklist check hits (same dimensions as the blacklist key)
	testx.True(t, riskinfra.IsRiskBlacklisted(ctx, m.rdb, userID, "5:mix_video"), "the pre-submission interception hits")
}
