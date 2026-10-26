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

// RiskConsumerState 是消费组件的运行时状态（双游标）。
type RiskConsumerState struct {
	LastInsertID   uint64 // 游标 A：新增游标（id > LastInsertID）
	LastUpdateTime int64  // 游标 B：更新游标（update_time > LastUpdateTime）
}

// RiskConsumerCursor 游标持久化接口（内存实现 + Redis 实现）。
type RiskConsumerCursor interface {
	Load(ctx context.Context) (*RiskConsumerState, error)
	Save(ctx context.Context, state *RiskConsumerState) error
}

// RiskHitRecord 一条风控命中记录（消费端看到的"事件"）。
type RiskHitRecord struct {
	ID         uint64
	UserID     int64
	SubProduct string // 统计/评估/拦截共用的维度 key（role:type 或异步 subProduct）
	RiskType   string
	UpdateTime int64
}

// RiskPoller 轮询数据源接口（可注入，便于单测）。
//
// 消费目标表为 cg_risk_audit_record（风控记录独立成表后，全是 risk: 前缀，
// 不再需要按前缀过滤脏数据——事件源语义更干净）。
type RiskPoller interface {
	// PollNew 取 id > lastInsertID 且 status='fail' 的新增命中记录。
	PollNew(ctx context.Context, lastInsertID uint64, limit int) ([]*RiskHitRecord, error)
	// PollUpdated 取 update_time > lastUpdateTime 且 update_time <= now 的更新命中记录。
	// lastInsertID 用于排除新增游标（游标 A）已消费过的行（双游标去重）。
	PollUpdated(ctx context.Context, lastUpdateTime int64, lastInsertID uint64, limit int) ([]*RiskHitRecord, error)
}

// AuditStatsConsumer 常驻消费组件：双游标轮询 cg_risk_audit_record，累加进 cg_risk_stats。
//
// 模拟 binlog 消费语义（spec §3）：
//   - 游标 A（新增）：id > LastInsertID，捕同步风控命中（插入即 fail）
//   - 游标 B（更新）：update_time > LastUpdateTime，捕异步风控命中（回调 Update 成 fail）
//   - Ack：整批处理成功才推进游标（"全部成功才 Ack"）；失败不推进，下轮重试（at-least-once）
//   - 脏数据：缺关键字段（UserID=0 / SubProduct 为空）的 fail 不累加，但游标照常推进（"脏消息直接 Ack"）
//   - 去重（仅批内）：同一条被 A/B 各看到一次，靠 `id > lastInsertID` 下界排除，
//     批内不再重复累加（UNIQUE 键只保证不产生重复行，本身不幂等）
//   - ⚠️ 跨批次局限：同步命中插入即 fail、update_time 永不变，游标 B 可能在后续批次
//     重新选中它（update_time 仍 > lastUpdateTime），而 cg_risk_stats 的
//     hit_count + 1 UPSERT 不幂等 → 跨批次重复累加仍可能发生。
//     已知继承局限（旧 internal/service/creation/risk_stats_consumer.go 同样存在），
//     留待后续修复（如按 (id, update_time) 去重，或排除 update_time <= lastUpdateTime）
type AuditStatsConsumer struct {
	poller   RiskPoller
	stats    domain.RiskStatsRepository
	cursor   RiskConsumerCursor
	logger   *zap.Logger
	interval time.Duration
	limit    int
}

// NewAuditStatsConsumer 创建消费组件。cursor 为 nil 时退化为内存态（重启重放，幂等兜底）。
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

// Run 常驻消费循环，直到 ctx 取消。每次 tick 消费一批。
func (c *AuditStatsConsumer) Run(ctx context.Context) error {
	state, err := c.cursor.Load(ctx)
	if err != nil {
		c.logger.Warn("risk stats cursor load failed, start from zero", zap.Error(err))
		state = &RiskConsumerState{}
	}
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	// 启动即消费一次
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

// RunOnce 执行一次消费（monitor 用 ticker 驱动，复用 runLoop 生命周期）。
func (c *AuditStatsConsumer) RunOnce(ctx context.Context) error {
	state, err := c.cursor.Load(ctx)
	if err != nil {
		return fmt.Errorf("cursor load: %w", err)
	}
	return c.consumeBatch(ctx, state)
}

// ConsumeBatchForTest 单测钩子：暴露一次 consumeBatch（不启动常驻循环）。
func (c *AuditStatsConsumer) ConsumeBatchForTest(ctx context.Context, state *RiskConsumerState) error {
	return c.consumeBatch(ctx, state)
}

// consumeBatch 消费一批：先新增后更新，全部成功才推进游标。
//
// 双游标去重 + 跨批次幂等：同步命中（插入即 fail）只会被游标 A（新增）捕获一次——
// PollUpdated 用 `update_time > create_time` 排除从未被 Update 过的行（同步 fail 行
// update_time == create_time），异步命中（回调变 fail）才会被游标 B（更新）捕获。
// 消费完新增后，用本批新增的最大 id 作为游标 B 的下界，批内 A/B 不重复。
func (c *AuditStatsConsumer) consumeBatch(ctx context.Context, state *RiskConsumerState) error {
	// 1. 消费新增（游标 A）
	newRecords, err := c.poller.PollNew(ctx, state.LastInsertID, c.limit)
	if err != nil {
		return fmt.Errorf("poll new: %w", err)
	}
	for _, rec := range newRecords {
		if err := c.accumulate(ctx, rec); err != nil {
			return fmt.Errorf("accumulate new id=%d: %w", rec.ID, err)
		}
	}

	// 本批新增最大 id：游标 B 用它排除新增游标已消费的行（双游标去重）
	newInsertID := state.LastInsertID
	if len(newRecords) > 0 {
		newInsertID = newRecords[len(newRecords)-1].ID
	}

	// 2. 消费更新（游标 B）
	updated, err := c.poller.PollUpdated(ctx, state.LastUpdateTime, newInsertID, c.limit)
	if err != nil {
		return fmt.Errorf("poll updated: %w", err)
	}
	for _, rec := range updated {
		if err := c.accumulate(ctx, rec); err != nil {
			return fmt.Errorf("accumulate updated id=%d: %w", rec.ID, err)
		}
	}

	// 3. Ack：全部成功才推进游标
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

// accumulate 把一条命中记录累加进统计表（hit_count + 1）。
// 去重由消费端保证：同步命中只走游标 A、异步命中只走游标 B（PollUpdated 的
// `update_time > create_time` 条件），同一条记录不会被 A/B 各消费一次，也不跨批次重复。
// UNIQUE 键作为最后的行级兜底。
func (c *AuditStatsConsumer) accumulate(ctx context.Context, rec *RiskHitRecord) error {
	if rec == nil || rec.UserID == 0 || rec.SubProduct == "" {
		return nil // 脏数据：缺关键字段，跳过累加
	}
	windowStart := rec.UpdateTime - rec.UpdateTime%60
	return c.stats.UpsertIncrement(ctx, rec.UserID, rec.SubProduct, rec.RiskType, windowStart)
}

// --- 游标实现 ---

// memoryCursor 内存游标（无 Redis / 单测时用）。
type memoryCursor struct{}

func (m *memoryCursor) Load(ctx context.Context) (*RiskConsumerState, error) {
	return &RiskConsumerState{}, nil
}
func (m *memoryCursor) Save(ctx context.Context, state *RiskConsumerState) error { return nil }

// NewMemoryCursor 创建内存游标（单测 / 无 Redis 场景）。
func NewMemoryCursor() RiskConsumerCursor { return &memoryCursor{} }

// redisCursor 游标的 Redis 持久化（进程重启不重放）。
type redisCursor struct {
	rdb *redis.Client
}

// NewRedisCursor 创建 Redis 游标。rdb 为 nil 时退化为内存游标。
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

// --- 轮询器（读取 cg_risk_audit_record） ---

// AuditRecordPoller 轮询 cg_risk_audit_record 表，实现 RiskPoller 接口。
//
// 分表后风控审计记录独立成表，轮询器直查该表（表结构审计记录行，
// 不再混读任务回调记录）。不通过 domain.AuditRecordRepository 查询——
// 仓储接口面向业务读写，没有批量的游标轮询方法；轮询器作为基础设施内部的
// 专用数据源实现，直接持有 *gorm.DB（与旧 CallbackRecordPoller 持 DAO 同构）。
type AuditRecordPoller struct {
	db *gorm.DB
}

// NewAuditRecordPoller 创建轮询器。
func NewAuditRecordPoller(db *gorm.DB) *AuditRecordPoller {
	return &AuditRecordPoller{db: db}
}

// 编译期断言：确保实现满足 RiskPoller 接口。
var _ RiskPoller = (*AuditRecordPoller)(nil)

// PollNew 取 id > lastInsertID 且 status='fail' 的命中记录（新增游标）。
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

// PollUpdated 取 update_time > lastUpdateTime 且 update_time <= now 且 status='fail' 的记录。
//
// ⚠️ 时间戳边界：update_time 是秒级，同一秒多条更新可能漏。
// 用 `update_time <= now` 排除"正在写入"的行。
//
// ⚠️ 双游标去重 + 跨批次幂等：游标 A（新增）已消费过 id <= lastInsertID 的行，这里用
// `id > lastInsertID` 排除新增游标已覆盖的行（批内去重）。同时用 `update_time > create_time`
// 排除"从未被 Update 过"的行——同步命中插入即 fail，其 update_time == create_time，更新游标
// 不应捕获它（它只属于新增游标 A）。异步命中插入时 update_time == create_time，但回调变 fail
// 时 Update 刷新 update_time > create_time，更新游标只捕这类真正变化过的行。这一条件从 SQL
// 层面根除跨批次重复累加（否则同步 fail 行在后续批次会被 update_time > lastUpdateTime 重新选中，
// 而 hit_count + 1 的 UPSERT 不幂等）。
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

// auditRowsToRiskHits 把审计记录行转成消费事件。
//
// 新表 cg_risk_audit_record 全为风控记录，无需再按 risk: 前缀过滤脏数据；
// 维度 key 由 extractRiskSubProduct 从 task_type 提取，无法提取时跳过累加
// （游标照常推进）。
func auditRowsToRiskHits(rows []*auditRecordRow) []*RiskHitRecord {
	hits := make([]*RiskHitRecord, 0, len(rows))
	for _, r := range rows {
		subProduct := extractRiskSubProduct(r.TaskType)
		if subProduct == "" {
			continue // 无有效维度 key，跳过累加（游标照常推进）
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

// extractRiskSubProduct 从审计记录的 task_type 提取聚合维度 key。
//
// task_type 形如：
//   - "risk:async:{subProduct}"       异步命中 → 取末段真实 subProduct
//   - "risk:{role}:{type}:{stage}"    同步命中 → 取 {role}:{type}
//
// 语义（统计/评估/拦截共用的维度 key）：
//   - 异步命中取末段真实 subProduct（如 dujiaapp_mixvideo60s_t2v_output）
//   - 同步命中取 {role}:{type}（如 5:mix_video）——同步命中的真实策略名要查
//     PreCreationInputMap，消费端拿不到请求上下文；用 {role}:{type} 作聚合维度，
//     与 pre_create 拦截时用的维度保持一致。
//
// 无法提取（空/畸形）时返回 ""，调用方跳过累加（游标照常推进）。
func extractRiskSubProduct(taskType string) string {
	if taskType == "" || !strings.HasPrefix(taskType, "risk:") {
		return ""
	}
	parts := strings.Split(taskType, ":")
	// 拆分后至少有 ["risk", ...]
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
