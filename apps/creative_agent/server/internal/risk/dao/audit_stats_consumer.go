package dao

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// RiskConsumerState is the consumer component's runtime state (dual cursors).
type RiskConsumerState struct {
	LastInsertID   uint64 // cursor A: insert cursor (id > LastInsertID)
	LastUpdateTime int64  // cursor B: update cursor (update_time > LastUpdateTime)
}

// RiskConsumerCursor is the cursor persistence interface (in-memory + Redis implementations).
type RiskConsumerCursor interface {
	Load(ctx context.Context) (*RiskConsumerState, error)
	Save(ctx context.Context, state *RiskConsumerState) error
}

// RiskHitRecord is one risk-hit record (an "event" as seen by the consumer).
type RiskHitRecord struct {
	ID         uint64
	UserID     int64
	SubProduct string // the dimension key shared by statistics/evaluation/interception (role:type or the async subProduct)
	RiskType   string
	UpdateTime int64
}

// RiskPoller is the polling data-source interface (injectable for unit tests).
//
// The consumption target table is cg_risk_audit_record (after risk records got their own table, everything has the risk: prefix,
// so prefix-based dirty-data filtering is no longer needed — cleaner event-source semantics).
type RiskPoller interface {
	// PollNew fetches new hit records with id > lastInsertID and status='fail'.
	PollNew(ctx context.Context, lastInsertID uint64, limit int) ([]*RiskHitRecord, error)
	// PollUpdated fetches updated hit records with update_time > lastUpdateTime and update_time <= now.
	// lastInsertID excludes rows already consumed by the insert cursor (cursor A) (dual-cursor dedup).
	PollUpdated(ctx context.Context, lastUpdateTime int64, lastInsertID uint64, limit int) ([]*RiskHitRecord, error)
}

// AuditStatsConsumer is the resident consumer: dual-cursor polling of cg_risk_audit_record, accumulating into cg_risk_stats.
//
// It simulates binlog consumption semantics (spec §3):
//   - Cursor A (inserts): id > LastInsertID, catching sync risk hits (inserted as fail)
//   - Cursor B (updates): update_time > LastUpdateTime, catching async risk hits (the callback Updates to fail)
//   - Ack: cursors advance only when the whole batch succeeds ("Ack only when all succeed"); failures do not advance and retry next round (at-least-once)
//   - Dirty data: fail records missing key fields (UserID=0 / empty SubProduct) are not accumulated, but cursors advance as usual ("Ack dirty messages directly")
//   - Dedup (in-batch only): the same record seen by both A/B is excluded by the `id > lastInsertID` lower bound,
//     so no duplicate accumulation within a batch (the UNIQUE key only prevents duplicate rows; it is not idempotent itself)
//   - ⚠️ Cross-batch limitation: sync hits are inserted as fail with update_time never changing, so cursor B may
//     re-select them in later batches (update_time still > lastUpdateTime), and cg_risk_stats's
//     hit_count + 1 UPSERT is not idempotent → cross-batch duplicate accumulation can still happen.
//     A known inherited limitation (the old internal/service/creation/risk_stats_consumer.go had it too),
//     left for a future fix (e.g. dedup by (id, update_time), or excluding update_time <= lastUpdateTime)
type AuditStatsConsumer struct {
	poller   RiskPoller
	stats    domain.RiskStatsRepository
	cursor   RiskConsumerCursor
	logger   *zap.Logger
	interval time.Duration
	limit    int
}

// NewAuditStatsConsumer creates the consumer component. When cursor is nil it degrades to in-memory state (replay on restart, idempotent fallback).
func NewAuditStatsConsumer(poller RiskPoller, stats domain.RiskStatsRepository, cursor RiskConsumerCursor, logger *zap.Logger) *AuditStatsConsumer {
	if cursor == nil {
		cursor = &memoryCursor{}
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &AuditStatsConsumer{
		poller:   poller,
		stats:    stats,
		cursor:   cursor,
		logger:   logger,
		interval: time.Second,
		limit:    100,
	}
}

// Run is the resident consumption loop until ctx is canceled. Each tick consumes one batch.
func (c *AuditStatsConsumer) Run(ctx context.Context) error {
	state, err := c.cursor.Load(ctx)
	if err != nil {
		c.logger.Warn("risk stats cursor load failed, start from zero", zap.Error(err))
		state = &RiskConsumerState{}
	}
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	// Consume once at startup
	if err := c.consumeBatch(ctx, state); err != nil {
		c.logger.Error("risk stats consumer batch failed", zap.Error(err))
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.consumeBatch(ctx, state); err != nil {
				c.logger.Error("risk stats consumer batch failed", zap.Error(err))
			}
		}
	}
}

// RunOnce performs one consumption round (the monitor drives it with a ticker, reusing the runLoop lifecycle).
func (c *AuditStatsConsumer) RunOnce(ctx context.Context) error {
	state, err := c.cursor.Load(ctx)
	if err != nil {
		return fmt.Errorf("cursor load: %w", err)
	}
	return c.consumeBatch(ctx, state)
}

// ConsumeBatchForTest is a unit-test hook: exposes one consumeBatch (without starting the resident loop).
func (c *AuditStatsConsumer) ConsumeBatchForTest(ctx context.Context, state *RiskConsumerState) error {
	return c.consumeBatch(ctx, state)
}

// consumeBatch consumes one batch: inserts first, then updates; cursors advance only when all succeed.
//
// Dual-cursor dedup + cross-batch idempotency: sync hits (inserted as fail) are captured only once by cursor A (inserts) —
// PollUpdated excludes rows never Updated with `update_time > create_time` (sync fail rows have
// update_time == create_time); only async hits (turned fail by callbacks) are captured by cursor B (updates).
// After consuming inserts, this batch's max insert id becomes cursor B's lower bound, so A/B do not overlap within the batch.
func (c *AuditStatsConsumer) consumeBatch(ctx context.Context, state *RiskConsumerState) error {
	// 1. Consume inserts (cursor A)
	newRecords, err := c.poller.PollNew(ctx, state.LastInsertID, c.limit)
	if err != nil {
		return fmt.Errorf("poll new: %w", err)
	}
	for _, rec := range newRecords {
		if err := c.accumulate(ctx, rec); err != nil {
			return fmt.Errorf("accumulate new id=%d: %w", rec.ID, err)
		}
	}

	// This batch's max insert id: cursor B uses it to exclude rows already consumed by the insert cursor (dual-cursor dedup)
	newInsertID := state.LastInsertID
	if len(newRecords) > 0 {
		newInsertID = newRecords[len(newRecords)-1].ID
	}

	// 2. Consume updates (cursor B)
	updated, err := c.poller.PollUpdated(ctx, state.LastUpdateTime, newInsertID, c.limit)
	if err != nil {
		return fmt.Errorf("poll updated: %w", err)
	}
	for _, rec := range updated {
		if err := c.accumulate(ctx, rec); err != nil {
			return fmt.Errorf("accumulate updated id=%d: %w", rec.ID, err)
		}
	}

	// 3. Ack: advance cursors only when all succeed
	newState := *state
	if len(newRecords) > 0 {
		newState.LastInsertID = newRecords[len(newRecords)-1].ID
	}
	if len(updated) > 0 {
		newState.LastUpdateTime = updated[len(updated)-1].UpdateTime
	}
	if newState != *state {
		if err := c.cursor.Save(ctx, &newState); err != nil {
			return fmt.Errorf("cursor save: %w", err)
		}
		*state = newState
	}
	return nil
}

// accumulate accumulates one hit record into the statistics table (hit_count + 1).
// Dedup is guaranteed by the consumer: sync hits go through cursor A only, async hits through cursor B only (PollUpdated's
// `update_time > create_time` condition), so one record is never consumed by both A/B nor duplicated across batches.
// The UNIQUE key is the last row-level fallback.
func (c *AuditStatsConsumer) accumulate(ctx context.Context, rec *RiskHitRecord) error {
	if rec == nil || rec.UserID == 0 || rec.SubProduct == "" {
		return nil // dirty data: missing key fields; skip accumulation
	}
	windowStart := rec.UpdateTime - rec.UpdateTime%60
	return c.stats.UpsertIncrement(ctx, rec.UserID, rec.SubProduct, rec.RiskType, windowStart)
}

// --- Cursor implementations ---

// memoryCursor is the in-memory cursor (no Redis / unit tests).
type memoryCursor struct{}

func (m *memoryCursor) Load(ctx context.Context) (*RiskConsumerState, error) {
	return &RiskConsumerState{}, nil
}
func (m *memoryCursor) Save(ctx context.Context, state *RiskConsumerState) error { return nil }

// NewMemoryCursor creates the in-memory cursor (unit tests / no-Redis scenarios).
func NewMemoryCursor() RiskConsumerCursor { return &memoryCursor{} }

// redisCursor is the Redis-persisted cursor (no replay across process restarts).
type redisCursor struct {
	rdb *redis.Client
}

// NewRedisCursor creates the Redis cursor. When rdb is nil it degrades to the in-memory cursor.
func NewRedisCursor(rdb *redis.Client) RiskConsumerCursor {
	if rdb == nil {
		return &memoryCursor{}
	}
	return &redisCursor{rdb: rdb}
}

const riskCursorKey = "risk:stats:cursor"

func (r *redisCursor) Load(ctx context.Context) (*RiskConsumerState, error) {
	vals, err := r.rdb.HMGet(ctx, riskCursorKey, "last_insert_id", "last_update_time").Result()
	if err != nil {
		return nil, err
	}
	state := &RiskConsumerState{}
	if len(vals) >= 2 {
		if s, ok := vals[0].(string); ok && s != "" {
			n, err := strconv.ParseUint(s, 10, 64)
			if err == nil {
				state.LastInsertID = n
			}
		}
		if s, ok := vals[1].(string); ok && s != "" {
			n, err := strconv.ParseInt(s, 10, 64)
			if err == nil {
				state.LastUpdateTime = n
			}
		}
	}
	return state, nil
}

func (r *redisCursor) Save(ctx context.Context, state *RiskConsumerState) error {
	return r.rdb.HSet(ctx, riskCursorKey,
		"last_insert_id", state.LastInsertID,
		"last_update_time", state.LastUpdateTime,
	).Err()
}

// --- Poller (reads cg_risk_audit_record) ---

// AuditRecordPoller polls the cg_risk_audit_record table, implementing the RiskPoller interface.
//
// After table splitting, risk-audit records have their own table; the poller queries it directly (audit-record rows,
// no longer mixed with task callback records). It does not query through domain.AuditRecordRepository —
// the repository interface serves business reads/writes and has no batch cursor-polling methods; the poller is an
// infrastructure-internal dedicated data source holding *gorm.DB directly (isomorphic to the old CallbackRecordPoller holding a DAO).
type AuditRecordPoller struct {
	db *gorm.DB
}

// NewAuditRecordPoller creates the poller.
func NewAuditRecordPoller(db *gorm.DB) *AuditRecordPoller {
	return &AuditRecordPoller{db: db}
}

// Compile-time assertion: the implementation satisfies the RiskPoller interface.
var _ RiskPoller = (*AuditRecordPoller)(nil)

// PollNew fetches hit records with id > lastInsertID and status='fail' (insert cursor).
func (p *AuditRecordPoller) PollNew(ctx context.Context, lastInsertID uint64, limit int) ([]*RiskHitRecord, error) {
	var rows []*auditRecordRow
	err := p.db.WithContext(ctx).
		Where("id > ? AND status = ?", lastInsertID, domain.StatusFail).
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return auditRowsToRiskHits(rows), nil
}

// PollUpdated fetches records with update_time > lastUpdateTime and update_time <= now and status='fail'.
//
// ⚠️ Timestamp boundary: update_time is second-granularity; multiple updates in the same second may be missed.
// `update_time <= now` excludes rows "being written".
//
// ⚠️ Dual-cursor dedup + cross-batch idempotency: cursor A (inserts) already consumed rows with id <= lastInsertID; here
// `id > lastInsertID` excludes rows covered by the insert cursor (in-batch dedup). Also `update_time > create_time`
// excludes rows "never Updated" — sync hits are inserted as fail with update_time == create_time, and the update cursor
// must not capture them (they belong to insert cursor A only). Async hits are inserted with update_time == create_time, but when the callback turns them fail
// the Update refreshes update_time > create_time, so the update cursor captures only these genuinely changed rows. This condition eradicates
// cross-batch duplicate accumulation at the SQL level (otherwise sync fail rows would be re-selected in later batches by update_time > lastUpdateTime,
// and the hit_count + 1 UPSERT is not idempotent).
func (p *AuditRecordPoller) PollUpdated(ctx context.Context, lastUpdateTime int64, lastInsertID uint64, limit int) ([]*RiskHitRecord, error) {
	now := time.Now().Unix()
	var rows []*auditRecordRow
	err := p.db.WithContext(ctx).
		Where("status = ? AND update_time > ? AND update_time <= ? AND update_time > create_time AND id > ?", domain.StatusFail, lastUpdateTime, now, lastInsertID).
		Order("update_time ASC, id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return auditRowsToRiskHits(rows), nil
}

// auditRowsToRiskHits converts audit-record rows into consumption events.
//
// The new table cg_risk_audit_record contains only risk records, so risk:-prefix dirty-data filtering is no longer needed;
// the dimension key is extracted from task_type by extractRiskSubProduct; when extraction fails, accumulation is skipped
// (cursors advance as usual).
func auditRowsToRiskHits(rows []*auditRecordRow) []*RiskHitRecord {
	hits := make([]*RiskHitRecord, 0, len(rows))
	for _, r := range rows {
		subProduct := extractRiskSubProduct(r.TaskType)
		if subProduct == "" {
			continue // no valid dimension key; skip accumulation (cursors advance as usual)
		}
		hits = append(hits, &RiskHitRecord{
			ID:         r.ID,
			UserID:     r.UserID,
			SubProduct: subProduct,
			RiskType:   "risk",
			UpdateTime: r.UpdateTime,
		})
	}
	return hits
}

// extractRiskSubProduct extracts the aggregation dimension key from an audit record's task_type.
//
// task_type looks like:
//   - "risk:async:{subProduct}"       async hit → take the trailing real subProduct
//   - "risk:{role}:{type}:{stage}"    sync hit → take {role}:{type}
//
// Semantics (the dimension key shared by statistics/evaluation/interception):
//   - Async hits take the trailing real subProduct (e.g. dujiaapp_mixvideo60s_t2v_output)
//   - Sync hits take {role}:{type} (e.g. 5:mix_video) — the real policy name of a sync hit requires looking up
//     PreCreationInputMap, and the consumer has no request context; using {role}:{type} as the aggregation dimension
//     stays consistent with the dimension used by pre_create interception.
//
// When extraction fails (empty/malformed) it returns "", and callers skip accumulation (cursors advance as usual).
func extractRiskSubProduct(taskType string) string {
	if taskType == "" || !strings.HasPrefix(taskType, "risk:") {
		return ""
	}
	parts := strings.Split(taskType, ":")
	// After splitting there is at least ["risk", ...]
	if len(parts) >= 3 && parts[1] == "async" {
		// risk:async:{subProduct}
		return parts[len(parts)-1]
	}
	if len(parts) >= 3 {
		// risk:{role}:{type}:{stage} → subProduct = role:type
		return parts[1] + ":" + parts[2]
	}
	return ""
}
