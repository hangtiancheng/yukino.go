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

// --- Stub implementations ---

// stubRiskStats is the in-memory domain.RiskStatsRepository.
// upserts records every UpsertIncrement call, used to assert accumulation counts under "dual-cursor dedup / dirty-data skip".
type stubRiskStats struct {
	mu        sync.Mutex
	upserts   []statsUpsert
	rows      []domain.RiskStatsRow // SumAllInWindow return value
	queryErr  error                 // error returned by SumAllInWindow
	upsertErr error                 // error returned by UpsertIncrement
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

// upsertKeys returns the deduplicated set of (userID, subProduct) pairs that were accumulated.
func (s *stubRiskStats) upsertKeys() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := map[string]bool{}
	for _, u := range s.upserts {
		keys[strconv.FormatInt(u.userID, 10)+":"+u.subProduct] = true
	}
	return keys
}

// stubPoller is the injectable test poller.
type stubPoller struct {
	mu        sync.Mutex
	newRecs   []*dao.RiskHitRecord
	updRecs   []*dao.RiskHitRecord
	pollNewN  int
	pollUpdN  int
	failCount int // the first N PollNew calls return errors
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
	// Replicate the real poller's dual-cursor dedup contract: id > lastInsertID excludes rows covered by the insert cursor
	out := make([]*dao.RiskHitRecord, 0, len(f.updRecs))
	for _, r := range f.updRecs {
		if r.ID > lastInsertID {
			out = append(out, r)
		}
	}
	return out, nil
}

// memoryCursorWithError is a cursor recording Save call counts / injectable Save errors.
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

// stubBlacklistWriter is a fake blacklist writer recording SetBlacklisted calls.
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

// --- Helpers ---

// newMockDB builds a sqlmock gorm.DB.
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

// --- consumeBatch: cursor advancement and dual-cursor dedup ---

func TestConsumeBatchAdvancesBothCursors(t *testing.T) {
	// 2 inserts (id advances to 10) + 1 update (update_time advances to 200) → all accumulated
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

	testx.Equal(t, uint64(10), state.LastInsertID, "the insert cursor advances to this batch's max id")
	testx.Equal(t, int64(200), state.LastUpdateTime, "the update cursor advances to this batch's max update_time")
	testx.Equal(t, 3, stats.upsertCount(), "the three hits each accumulate once")

	keys := stats.upsertKeys()
	testx.Contains(t, keys, key(100, "5:mix_video"))
	testx.Contains(t, keys, key(101, "5:mix_video"))
	testx.Contains(t, keys, key(200, "dujiaapp_mixvideo60s_t2v_output"))
}

func TestConsumeBatchPollUpdatedUsesNewCursorAsLowerBound(t *testing.T) {
	// Dual-cursor dedup: cursor B's PollUpdated lower bound must be "this batch's max insert id", not the pre-advance LastInsertID.
	// The poller layer already excludes with id > lastInsertID → the same record seen by both A/B accumulates only once.
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

	testx.Equal(t, 1, stats.upsertCount(), "a record seen by both A/B within one batch accumulates once")
	testx.Contains(t, stats.upsertKeys(), key(100, "5:mix_video"))
}

func TestConsumeBatchNoAdvanceOnAccumulateFailure(t *testing.T) {
	// Accumulation failure does not advance cursors (at-least-once: retry next round)
	stats := &stubRiskStats{upsertErr: errors.New("duplicate insert failed")}
	poller := &stubPoller{
		newRecs: []*dao.RiskHitRecord{
			{ID: 1, UserID: 100, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 100},
		},
	}
	consumer := dao.NewAuditStatsConsumer(poller, stats, dao.NewMemoryCursor(), nil)
	state := &dao.RiskConsumerState{}
	err := consumer.ConsumeBatchForTest(context.Background(), state)
	testx.Error(t, err, "an accumulation failure should return an error")
	testx.Equal(t, uint64(0), state.LastInsertID, "failure does not advance the insert cursor")
	testx.Equal(t, int64(0), state.LastUpdateTime, "failure does not advance the update cursor")
}

func TestConsumeBatchNoAdvanceOnPollUpdatedFailure(t *testing.T) {
	// Update-poll failure: inserts were accumulated but no cursor advances (whole-batch Ack semantics)
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
	testx.Error(t, err, "an update-poll failure should return an error")
	testx.Equal(t, uint64(0), state.LastInsertID, "a whole-batch failure does not advance cursors (next-round duplicate accumulation is covered by the UNIQUE fallback)")
	testx.Equal(t, 1, stats.upsertCount(), "the insert record was accumulated (but cursors did not advance)")
}

func TestConsumeBatchSkipsDirtyDataButAdvancesCursor(t *testing.T) {
	// Dirty data (missing userID / subProduct) skips accumulation, but cursors advance as usual (dirty messages Ack directly)
	stats := &stubRiskStats{}
	poller := &stubPoller{
		newRecs: []*dao.RiskHitRecord{
			{ID: 5, UserID: 0, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 100},   // missing userID
			{ID: 6, UserID: 102, SubProduct: "", RiskType: "risk", UpdateTime: 100},            // missing subProduct
			{ID: 7, UserID: 103, SubProduct: "5:mix_video", RiskType: "risk", UpdateTime: 101}, // normal
		},
	}
	consumer := dao.NewAuditStatsConsumer(poller, stats, dao.NewMemoryCursor(), nil)
	state := &dao.RiskConsumerState{}
	testx.NoError(t, consumer.ConsumeBatchForTest(context.Background(), state))

	testx.Equal(t, uint64(7), state.LastInsertID, "dirty data does not block cursor advancement")
	testx.Equal(t, 1, stats.upsertCount(), "only the normal record is accumulated")
	testx.Contains(t, stats.upsertKeys(), key(103, "5:mix_video"))
}

func TestConsumeBatchCursorSaveOnSuccess(t *testing.T) {
	// Ack-on-success: successful processing advances and saves cursors
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
	testx.Equal(t, 1, cursor.saves, "successful processing should advance and save cursors")
	testx.Equal(t, uint64(7), state.LastInsertID, "the in-memory state advances in sync")
}

func TestConsumeBatchCursorSaveFailureStopsBatch(t *testing.T) {
	// Cursor save failure: returns an error and the in-memory state does not advance (retry next round)
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
	testx.Error(t, err, "a cursor save failure should return an error")
	testx.Equal(t, uint64(0), state.LastInsertID, "a save failure does not advance the in-memory state")
}

func TestConsumeBatchNoOpWhenEmpty(t *testing.T) {
	// No inserts, no updates: no accumulation, no cursor save
	cursor := &memoryCursorWithError{}
	stats := &stubRiskStats{}
	poller := &stubPoller{}
	consumer := dao.NewAuditStatsConsumer(poller, stats, cursor, nil)
	state := &dao.RiskConsumerState{LastInsertID: 3, LastUpdateTime: 500}
	testx.NoError(t, consumer.ConsumeBatchForTest(context.Background(), state))

	testx.Equal(t, uint64(3), state.LastInsertID, "cursors stay put without new data")
	testx.Equal(t, int64(500), state.LastUpdateTime)
	testx.Equal(t, 0, stats.upsertCount())
	testx.Equal(t, 0, cursor.saves, "no new data triggers no cursor save")
}

// --- Cursors: Redis degradation path ---

func TestNewRedisCursorNilFallsBackToMemory(t *testing.T) {
	// rdb nil → degrades to the in-memory cursor: Load returns the empty state, Save is a no-op
	cursor := dao.NewRedisCursor(nil)
	state, err := cursor.Load(context.Background())
	testx.NoError(t, err)
	testx.Equal(t, &dao.RiskConsumerState{}, state)
	testx.NoError(t, cursor.Save(context.Background(), &dao.RiskConsumerState{LastInsertID: 9, LastUpdateTime: 100}))
}

// --- AuditRecordPoller: subProduct extraction + dual-cursor lower bounds ---

func TestAuditRecordPollerPollNewExtractsSubProduct(t *testing.T) {
	gdb, mock := newMockDB(t)
	poller := dao.NewAuditRecordPoller(gdb)

	mock.ExpectQuery("SELECT \\* FROM `cg_risk_audit_record` WHERE id > \\? AND status = \\? ORDER BY id ASC LIMIT \\?").
		WithArgs(sqlmock.AnyArg(), "fail", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "task_type", "update_time"}).
			AddRow(1, 100, "risk:5:mix_video:precreate", 100).
			AddRow(2, 200, "risk:async:dujiaapp_mixvideo60s_t2v_output", 200).
			AddRow(3, 300, "5:mix_video", 300)) // non-risk prefix (task callback) → skipped

	hits, err := poller.PollNew(context.Background(), 0, 100)
	testx.NoError(t, err)
	testx.NoError(t, mock.ExpectationsWereMet())
	testx.Len(t, hits, 2, "non-risk-prefix records are filtered")
	testx.Equal(t, "5:mix_video", hits[0].SubProduct, "sync hits take {role}:{type}")
	testx.Equal(t, uint64(1), hits[0].ID)
	testx.Equal(t, int64(100), hits[0].UserID)
	testx.Equal(t, "dujiaapp_mixvideo60s_t2v_output", hits[1].SubProduct, "async hits take the trailing real subProduct")
	testx.Equal(t, int64(200), hits[1].UpdateTime)
}

func TestAuditRecordPollerPollUpdatedUsesInsertLowerBound(t *testing.T) {
	// Dual-cursor dedup: PollUpdated must carry the id > lastInsertID condition (excluding rows consumed by the insert cursor)
	// and update_time > create_time (excluding sync fail rows never Updated, cross-batch idempotency).
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
	// Cross-batch dedup: sync fail rows are inserted as fail with update_time == create_time (never Updated),
	// already captured by insert cursor A. Cursor B's PollUpdated must exclude such rows (update_time > create_time),
	// otherwise later batches would re-select them via update_time > lastUpdateTime, and hit_count + 1 would duplicate non-idempotently.
	// Async hit rows (the callback Updates to fail, update_time > create_time) must still be selected.
	gdb, mock := newMockDB(t)
	poller := dao.NewAuditRecordPoller(gdb)

	mock.ExpectQuery("SELECT \\* FROM `cg_risk_audit_record` WHERE status = \\? AND update_time > \\? AND update_time <= \\? AND update_time > create_time AND id > \\? ORDER BY update_time ASC, id ASC LIMIT \\?").
		WithArgs("fail", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "task_type", "update_time"}).
			AddRow(8, 400, "risk:async:dujiaapp_miaotu_t2i_output", 300))

	hits, err := poller.PollUpdated(context.Background(), 100, 7, 100)
	testx.NoError(t, err)
	testx.NoError(t, mock.ExpectationsWereMet())
	testx.Len(t, hits, 1, "async hit rows (update_time > create_time) should be selected by the update cursor")
	testx.Equal(t, "dujiaapp_miaotu_t2i_output", hits[0].SubProduct)
}

// --- RiskOfflineJob: threshold evaluation + blacklist ---

func TestRiskOfflineJobWritesBlacklistOverThreshold(t *testing.T) {
	stats := newStubRiskStats(
		domain.RiskStatsRow{UserID: 1, SubProduct: "5:mix_video", HitCount: 6}, // >5 → offline
		domain.RiskStatsRow{UserID: 2, SubProduct: "precreate", HitCount: 5},   // =5 → offline
		domain.RiskStatsRow{UserID: 3, SubProduct: "5:mix_video", HitCount: 4}, // <5 → stays online
	)
	writer := &stubBlacklistWriter{}
	job := dao.NewRiskOfflineJob(stats, writer)

	testx.NoError(t, job.Run(context.Background()))
	testx.True(t, writer.blacklisted(1, "5:mix_video"), "over the threshold should write the blacklist")
	testx.True(t, writer.blacklisted(2, "precreate"), "at the threshold should write the blacklist")
	testx.False(t, writer.blacklisted(3, "5:mix_video"), "under the threshold should not write the blacklist")
}

func TestRiskOfflineJobNilWriterSkipsWrite(t *testing.T) {
	// blacklist not injected (nil) → evaluation runs as usual, no panic, no writes
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
	// Compatibility entry: rdb nil → evaluation runs as usual, no blacklist writes, no panic
	stats := newStubRiskStats(
		domain.RiskStatsRow{UserID: 1, SubProduct: "5:mix_video", HitCount: 10},
	)
	testx.NoError(t, dao.RunRiskOfflineJob(context.Background(), stats, nil))
}
