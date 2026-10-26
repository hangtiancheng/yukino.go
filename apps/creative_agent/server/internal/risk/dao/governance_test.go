package dao_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// --- stub 实现 ---

// stubRiskStats 内存版 domain.RiskStatsRepository。
// upserts 记录每次 UpsertIncrement 调用，用于断言"双游标去重/脏数据跳过"的累加次数。
type stubRiskStats struct {
	mu        sync.Mutex
	upserts   []statsUpsert
	rows      []domain.RiskStatsRow // SumAllInWindow 返回值
	queryErr  error                 // SumAllInWindow 返回的错误
	upsertErr error                 // UpsertIncrement 返回的错误
}

type statsUpsert struct {
	userID      int64
	subProduct  string
	riskType    string
	windowStart int64
}

func newStubRiskStats(rows ...domain.RiskStatsRow) *stubRiskStats {
	return &stubRiskStats{rows: rows}
}

func (s *stubRiskStats) UpsertIncrement(ctx context.Context, userID int64, subProduct, riskType string, windowStart int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserts = append(s.upserts, statsUpsert{userID: userID, subProduct: subProduct, riskType: riskType, windowStart: windowStart})
	return nil
}

func (s *stubRiskStats) SumByUserAndSubProduct(ctx context.Context, userID int64, subProduct string, windowStart int64) (int, error) {
	return 0, nil
}

func (s *stubRiskStats) SumAllInWindow(ctx context.Context, windowStart int64) ([]domain.RiskStatsRow, error) {
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	return s.rows, nil
}

func (s *stubRiskStats) MinWindowStart(ctx context.Context) (int64, error) {
	return 0, nil
}

func (s *stubRiskStats) upsertCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.upserts)
}

// upsertKeys 返回被累加过的 (userID, subProduct) 去重集合。
func (s *stubRiskStats) upsertKeys() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := map[string]bool{}
	for _, u := range s.upserts {
		keys[strconv.FormatInt(u.userID, 10)+":"+u.subProduct] = true
	}
	return keys
}

// stubPoller 可注入的测试 poller。
type stubPoller struct {
	mu        sync.Mutex
	newRecs   []*dao.RiskHitRecord
	updRecs   []*dao.RiskHitRecord
	pollNewN  int
	pollUpdN  int
	failCount int // 前 N 次 PollNew 返回错误
	updErr    error
}

func (f *stubPoller) PollNew(ctx context.Context, lastInsertID uint64, limit int) ([]*dao.RiskHitRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pollNewN++
	if f.failCount > 0 {
		f.failCount--
		return nil, errors.New("db down")
	}
	return f.newRecs, nil
}

func (f *stubPoller) PollUpdated(ctx context.Context, lastUpdateTime int64, lastInsertID uint64, limit int) ([]*dao.RiskHitRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pollUpdN++
	if f.updErr != nil {
		return nil, f.updErr
	}
	// 复刻真实 poller 的双游标去重契约：id > lastInsertID 排除新增游标已覆盖的行
	out := make([]*dao.RiskHitRecord, 0, len(f.updRecs))
	for _, r := range f.updRecs {
		if r.ID > lastInsertID {
			out = append(out, r)
		}
	}
	return out, nil
}

// memoryCursorWithError 记录 Save 调用次数/可注入 Save 错误的游标。
type memoryCursorWithError struct {
	saves int
	err   error
}

func (m *memoryCursorWithError) Load(ctx context.Context) (*dao.RiskConsumerState, error) {
	return &dao.RiskConsumerState{}, nil
}
func (m *memoryCursorWithError) Save(ctx context.Context, state *dao.RiskConsumerState) error {
	m.saves++
	return m.err
}

// stubBlacklistWriter 记录 SetBlacklisted 调用的假黑名单写入器。
type stubBlacklistWriter struct {
	mu      sync.Mutex
	entries []blacklistEntry
}

type blacklistEntry struct {
	userID     int64
	subProduct string
}

func (w *stubBlacklistWriter) SetBlacklisted(ctx context.Context, userID int64, subProduct string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries = append(w.entries, blacklistEntry{userID: userID, subProduct: subProduct})
	return nil
}

func (w *stubBlacklistWriter) blacklisted(userID int64, subProduct string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, e := range w.entries {
		if e.userID == userID && e.subProduct == subProduct {
			return true
		}
	}
	return false
}

// --- 工具 ---

// newMockDB 建 sqlmock 的 gorm.DB。
func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	testx.NoError(t, err)
	gdb, err := gorm.Open(mysql.New(mysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{})
	testx.NoError(t, err)
	return gdb, mock
}

func key(userID int64, subProduct string) string {
	return strconv.FormatInt(userID, 10) + ":" + subProduct
}

// --- consumeBatch：游标推进与双游标去重 ---

func TestConsumeBatchAdvancesBothCursors(t *testing.T) {
	// 新增 2 条（id 推进到 10）+ 更新 1 条（update_time 推进到 200）→ 全部累加
	stats := &stubRiskStats{}
	poller := &stubPoller{
		newRecs: []*dao.RiskHitRecord{
			{ID: 9, UserID: 100, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 100},
			{ID: 10, UserID: 101, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 101},
		},
		updRecs: []*dao.RiskHitRecord{
			{ID: 11, UserID: 200, SubProduct: "dujiaapp_mixvideo60s_t2v_output", RiskType: "risk", UpdateTime: 200},
		},
	}
	consumer := dao.NewAuditStatsConsumer(poller, stats, dao.NewMemoryCursor(), nil)
	state := &dao.RiskConsumerState{}
	testx.NoError(t, consumer.ConsumeBatchForTest(context.Background(), state))

	testx.Equal(t, uint64(10), state.LastInsertID, "新增游标推进到本批最大 id")
	testx.Equal(t, int64(200), state.LastUpdateTime, "更新游标推进到本批最大 update_time")
	testx.Equal(t, 3, stats.upsertCount(), "三条命中各累加一次")

	keys := stats.upsertKeys()
	testx.Contains(t, keys, key(100, "5:mix_video"))
	testx.Contains(t, keys, key(101, "5:mix_video"))
	testx.Contains(t, keys, key(200, "dujiaapp_mixvideo60s_t2v_output"))
}

func TestConsumeBatchPollUpdatedUsesNewCursorAsLowerBound(t *testing.T) {
	// 双游标去重：游标 B 的 PollUpdated 下界必须是"本批新增最大 id"而非推进前 LastInsertID。
	// poller 层已用 id > lastInsertID 排除 → 同一条被 A/B 各看到也只累加 1 次。
	stats := &stubRiskStats{}
	poller := &stubPoller{
		newRecs: []*dao.RiskHitRecord{
			{ID: 7, UserID: 100, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 100},
		},
		updRecs: []*dao.RiskHitRecord{
			{ID: 7, UserID: 100, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 100},
		},
	}
	consumer := dao.NewAuditStatsConsumer(poller, stats, dao.NewMemoryCursor(), nil)
	state := &dao.RiskConsumerState{}
	testx.NoError(t, consumer.ConsumeBatchForTest(context.Background(), state))

	testx.Equal(t, 1, stats.upsertCount(), "同一批内 A/B 重复出现的记录只累加一次")
	testx.Contains(t, stats.upsertKeys(), key(100, "5:mix_video"))
}

func TestConsumeBatchNoAdvanceOnAccumulateFailure(t *testing.T) {
	// 累加失败不推进游标（at-least-once：失败下轮重试）
	stats := &stubRiskStats{upsertErr: errors.New("duplicate insert failed")}
	poller := &stubPoller{
		newRecs: []*dao.RiskHitRecord{
			{ID: 1, UserID: 100, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 100},
		},
	}
	consumer := dao.NewAuditStatsConsumer(poller, stats, dao.NewMemoryCursor(), nil)
	state := &dao.RiskConsumerState{}
	err := consumer.ConsumeBatchForTest(context.Background(), state)
	testx.Error(t, err, "累加失败应返回错误")
	testx.Equal(t, uint64(0), state.LastInsertID, "失败不推进新增游标")
	testx.Equal(t, int64(0), state.LastUpdateTime, "失败不推进更新游标")
}

func TestConsumeBatchNoAdvanceOnPollUpdatedFailure(t *testing.T) {
	// 更新轮询失败：新增已累加但游标整体不推进（整批 Ack 语义）
	stats := &stubRiskStats{}
	poller := &stubPoller{
		newRecs: []*dao.RiskHitRecord{
			{ID: 7, UserID: 100, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 100},
		},
		updErr: errors.New("db down"),
	}
	consumer := dao.NewAuditStatsConsumer(poller, stats, dao.NewMemoryCursor(), nil)
	state := &dao.RiskConsumerState{}
	err := consumer.ConsumeBatchForTest(context.Background(), state)
	testx.Error(t, err, "更新轮询失败应返回错误")
	testx.Equal(t, uint64(0), state.LastInsertID, "整批失败不推进游标（下轮重试重复累加靠 UNIQUE 幂等兜底）")
	testx.Equal(t, 1, stats.upsertCount(), "新增记录已被累加（但游标未推进）")
}

func TestConsumeBatchSkipsDirtyDataButAdvancesCursor(t *testing.T) {
	// 脏数据（缺 userID / subProduct）跳过累加，但游标照常推进（脏消息直接 Ack）
	stats := &stubRiskStats{}
	poller := &stubPoller{
		newRecs: []*dao.RiskHitRecord{
			{ID: 5, UserID: 0, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 100},   // 缺 userID
			{ID: 6, UserID: 102, SubProduct: "", RiskType: "risk", UpdateTime: 100},            // 缺 subProduct
			{ID: 7, UserID: 103, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 101}, // 正常
		},
	}
	consumer := dao.NewAuditStatsConsumer(poller, stats, dao.NewMemoryCursor(), nil)
	state := &dao.RiskConsumerState{}
	testx.NoError(t, consumer.ConsumeBatchForTest(context.Background(), state))

	testx.Equal(t, uint64(7), state.LastInsertID, "脏数据不阻断游标推进")
	testx.Equal(t, 1, stats.upsertCount(), "只有正常记录被累加")
	testx.Contains(t, stats.upsertKeys(), key(103, "5:mix_video"))
}

func TestConsumeBatchCursorSaveOnSuccess(t *testing.T) {
	// Ack-on-success：处理成功推进并保存游标
	cursor := &memoryCursorWithError{}
	stats := &stubRiskStats{}
	poller := &stubPoller{
		newRecs: []*dao.RiskHitRecord{
			{ID: 7, UserID: 100, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 100},
		},
	}
	consumer := dao.NewAuditStatsConsumer(poller, stats, cursor, nil)
	state := &dao.RiskConsumerState{}
	testx.NoError(t, consumer.ConsumeBatchForTest(context.Background(), state))
	testx.Equal(t, 1, cursor.saves, "处理成功应推进并保存游标")
	testx.Equal(t, uint64(7), state.LastInsertID, "内存态同步推进")
}

func TestConsumeBatchCursorSaveFailureStopsBatch(t *testing.T) {
	// 游标保存失败：返回错误且内存态不推进（下轮重试）
	cursor := &memoryCursorWithError{err: errors.New("redis down")}
	stats := &stubRiskStats{}
	poller := &stubPoller{
		newRecs: []*dao.RiskHitRecord{
			{ID: 7, UserID: 100, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 100},
		},
	}
	consumer := dao.NewAuditStatsConsumer(poller, stats, cursor, nil)
	state := &dao.RiskConsumerState{}
	err := consumer.ConsumeBatchForTest(context.Background(), state)
	testx.Error(t, err, "游标保存失败应返回错误")
	testx.Equal(t, uint64(0), state.LastInsertID, "保存失败不推进内存态")
}

func TestConsumeBatchNoOpWhenEmpty(t *testing.T) {
	// 无新增无更新：不累加、不保存游标
	cursor := &memoryCursorWithError{}
	stats := &stubRiskStats{}
	poller := &stubPoller{}
	consumer := dao.NewAuditStatsConsumer(poller, stats, cursor, nil)
	state := &dao.RiskConsumerState{LastInsertID: 3, LastUpdateTime: 500}
	testx.NoError(t, consumer.ConsumeBatchForTest(context.Background(), state))

	testx.Equal(t, uint64(3), state.LastInsertID, "无新数据游标不动")
	testx.Equal(t, int64(500), state.LastUpdateTime)
	testx.Equal(t, 0, stats.upsertCount())
	testx.Equal(t, 0, cursor.saves, "无新数据不触发游标保存")
}

// --- 游标：Redis 退化路径 ---

func TestNewRedisCursorNilFallsBackToMemory(t *testing.T) {
	// rdb 为 nil → 退化为内存游标：Load 返回空态、Save 无操作
	cursor := dao.NewRedisCursor(nil)
	state, err := cursor.Load(context.Background())
	testx.NoError(t, err)
	testx.Equal(t, &dao.RiskConsumerState{}, state)
	testx.NoError(t, cursor.Save(context.Background(), &dao.RiskConsumerState{LastInsertID: 9, LastUpdateTime: 100}))
}

// --- AuditRecordPoller：subProduct 提取 + 双游标下界 ---

func TestAuditRecordPollerPollNewExtractsSubProduct(t *testing.T) {
	gdb, mock := newMockDB(t)
	poller := dao.NewAuditRecordPoller(gdb)

	mock.ExpectQuery("SELECT \\* FROM `cg_risk_audit_record` WHERE id > \\? AND status = \\? ORDER BY id ASC LIMIT \\?").
		WithArgs(sqlmock.AnyArg(), "fail", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "task_type", "update_time"}).
			AddRow(1, 100, "risk:5:mix_video:precreate", 100).
			AddRow(2, 200, "risk:async:dujiaapp_mixvideo60s_t2v_output", 200).
			AddRow(3, 300, "5:mix_video", 300)) // 非 risk 前缀（任务回调）→ 跳过

	hits, err := poller.PollNew(context.Background(), 0, 100)
	testx.NoError(t, err)
	testx.NoError(t, mock.ExpectationsWereMet())
	testx.Len(t, hits, 2, "非 risk 前缀记录被过滤")
	testx.Equal(t, "5:mix_video", hits[0].SubProduct, "同步命中取 {role}:{type}")
	testx.Equal(t, uint64(1), hits[0].ID)
	testx.Equal(t, int64(100), hits[0].UserID)
	testx.Equal(t, "dujiaapp_mixvideo60s_t2v_output", hits[1].SubProduct, "异步命中取末段真实 subProduct")
	testx.Equal(t, int64(200), hits[1].UpdateTime)
}

func TestAuditRecordPollerPollUpdatedUsesInsertLowerBound(t *testing.T) {
	// 双游标去重：PollUpdated 必须带 id > lastInsertID 条件（排除新增游标已消费的行）
	// 且带 update_time > create_time（排除从未被 Update 过的同步 fail 行，跨批次幂等）。
	gdb, mock := newMockDB(t)
	poller := dao.NewAuditRecordPoller(gdb)

	mock.ExpectQuery("SELECT \\* FROM `cg_risk_audit_record` WHERE status = \\? AND update_time > \\? AND update_time <= \\? AND update_time > create_time AND id > \\? ORDER BY update_time ASC, id ASC LIMIT \\?").
		WithArgs("fail", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "task_type", "update_time"}).
			AddRow(8, 400, "risk:async:dujiaapp_miaotu_t2i_output", 300))

	hits, err := poller.PollUpdated(context.Background(), 100, 7, 100)
	testx.NoError(t, err)
	testx.NoError(t, mock.ExpectationsWereMet())
	testx.Len(t, hits, 1)
	testx.Equal(t, "dujiaapp_miaotu_t2i_output", hits[0].SubProduct)
}

func TestAuditRecordPollerPollUpdatedExcludesNeverUpdatedRows(t *testing.T) {
	// 跨批次去重：同步 fail 行插入即 fail，update_time == create_time（从未被 Update 过），
	// 已由新增游标 A 捕获。更新游标 B 的 PollUpdated 必须排除这类行（update_time > create_time），
	// 否则后续批次 update_time > lastUpdateTime 会重新选中它，hit_count + 1 非幂等重复累加。
	// 异步命中行（回调 Update 变 fail，update_time > create_time）必须仍被选中。
	gdb, mock := newMockDB(t)
	poller := dao.NewAuditRecordPoller(gdb)

	mock.ExpectQuery("SELECT \\* FROM `cg_risk_audit_record` WHERE status = \\? AND update_time > \\? AND update_time <= \\? AND update_time > create_time AND id > \\? ORDER BY update_time ASC, id ASC LIMIT \\?").
		WithArgs("fail", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "task_type", "update_time"}).
			AddRow(8, 400, "risk:async:dujiaapp_miaotu_t2i_output", 300))

	hits, err := poller.PollUpdated(context.Background(), 100, 7, 100)
	testx.NoError(t, err)
	testx.NoError(t, mock.ExpectationsWereMet())
	testx.Len(t, hits, 1, "异步命中行（update_time > create_time）应被更新游标选中")
	testx.Equal(t, "dujiaapp_miaotu_t2i_output", hits[0].SubProduct)
}

// --- RiskOfflineJob：阈值评估 + 黑名单 ---

func TestRiskOfflineJobWritesBlacklistOverThreshold(t *testing.T) {
	stats := newStubRiskStats(
		domain.RiskStatsRow{UserID: 1, SubProduct: "5:mix_video", HitCount: 6}, // >5 → 下线
		domain.RiskStatsRow{UserID: 2, SubProduct: "precreate", HitCount: 5},   // =5 → 下线
		domain.RiskStatsRow{UserID: 3, SubProduct: "5:mix_video", HitCount: 4}, // <5 → 不下线
	)
	writer := &stubBlacklistWriter{}
	job := dao.NewRiskOfflineJob(stats, writer)

	testx.NoError(t, job.Run(context.Background()))
	testx.True(t, writer.blacklisted(1, "5:mix_video"), "超过阈值应写黑名单")
	testx.True(t, writer.blacklisted(2, "precreate"), "等于阈值应写黑名单")
	testx.False(t, writer.blacklisted(3, "5:mix_video"), "低于阈值不应写黑名单")
}

func TestRiskOfflineJobNilWriterSkipsWrite(t *testing.T) {
	// blacklist 未注入（nil）→ 评估照常跑，不 panic、不写
	stats := newStubRiskStats(
		domain.RiskStatsRow{UserID: 1, SubProduct: "5:mix_video", HitCount: 10},
	)
	job := dao.NewRiskOfflineJob(stats, nil)
	testx.NoError(t, job.Run(context.Background()))
}

func TestRiskOfflineJobQueryErrorPropagates(t *testing.T) {
	stats := &stubRiskStats{queryErr: errors.New("db down")}
	job := dao.NewRiskOfflineJob(stats, &stubBlacklistWriter{})
	testx.Error(t, job.Run(context.Background()))
}

func TestRiskOfflineJobNilStatsSkips(t *testing.T) {
	job := dao.NewRiskOfflineJob(nil, &stubBlacklistWriter{})
	testx.NoError(t, job.Run(context.Background()))
}

func TestRiskOfflineJobRunRiskOfflineJobNilRedis(t *testing.T) {
	// 兼容入口：rdb 为 nil → 评估照常、不写黑名单、不 panic
	stats := newStubRiskStats(
		domain.RiskStatsRow{UserID: 1, SubProduct: "5:mix_video", HitCount: 10},
	)
	testx.NoError(t, dao.RunRiskOfflineJob(context.Background(), stats, nil))
}
