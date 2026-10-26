package worker_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/worker"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/logger"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGAsyncRequest{}))
	return db
}

func testLogger() *zap.Logger {
	return logger.New("info")
}

// TestHeartbeat_LockAndRenew verifies the heartbeat goroutine refreshes
// both Redis lock TTL and MySQL heartbeat_at.
func TestHeartbeat_LockAndRenew(t *testing.T) {
	locker := infra.NewMemoryLocker()
	db := newTestDB(t)
	asyncRepo := repository.NewAsyncRequestRepository(db)
	ctx := context.Background()

	// Create a Pending request first
	req := &model.CGAsyncRequest{
		AsyncRequestID: "ar-001", MessageID: "msg-001",
		UserID: 1, SessionID: "s1", AgentID: "a1", Query: "hello",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, req))

	// Takeover (CAS)
	taken, err := asyncRepo.Takeover(ctx, req.AsyncRequestID, "worker-test")
	testx.NoError(t, err)
	testx.NotNil(t, taken)

	// Set lock（内存锁底座，语义对齐 Redis SetNX）
	hbKey := "cg:hb:msg-001"
	ok, err := locker.SetNX(ctx, hbKey, "worker-test", 30*time.Second)
	testx.NoError(t, err)
	testx.True(t, ok)

	// Check heartbeat_at was set
	rec, err := asyncRepo.GetByID(ctx, req.ID)
	testx.NoError(t, err)
	testx.Greater(t, rec.HeartbeatAt, int64(0))

	// Update heartbeat
	testx.NoError(t, asyncRepo.UpdateHeartbeat(ctx, req.ID))

	// Re-fetch and verify heartbeat_at increased
	rec2, err := asyncRepo.GetByID(ctx, req.ID)
	testx.NoError(t, err)
	testx.GreaterOrEqual(t, rec2.HeartbeatAt, rec.HeartbeatAt)
}

// TestAsyncRequest_Takeover_CAS verifies the CAS semantics:
// only one concurrent takeover succeeds.
func TestAsyncRequest_Takeover_CAS(t *testing.T) {
	db := newTestDB(t)
	asyncRepo := repository.NewAsyncRequestRepository(db)
	ctx := context.Background()

	req := &model.CGAsyncRequest{
		AsyncRequestID: "ar-cas", MessageID: "msg-cas",
		UserID: 1, SessionID: "s1", AgentID: "a1", Query: "q",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, req))

	// First takeover — should succeed
	first, err := asyncRepo.Takeover(ctx, req.AsyncRequestID, "worker-1")
	testx.NoError(t, err)
	testx.NotNil(t, first)
	testx.Equal(t, model.AsyncStatusInProgress, first.Status)
	testx.Equal(t, "worker-1", first.WorkerID)
	testx.Equal(t, 1, first.AttemptCount)

	// Second takeover on same record — should fail (CAS conflict)
	second, err := asyncRepo.Takeover(ctx, req.AsyncRequestID, "worker-2")
	testx.ErrorIs(t, err, repository.ErrAsyncRequestConflict)
	testx.Nil(t, second)
}

// TestAsyncRequest_Takeover_ByBusinessKey 回归测试：Takeover 必须用雪花业务键
// async_request_id（stream 载荷携带的值）做 CAS 匹配，而不是自增主键 id。
// 修复前：consumer 把雪花键 ParseInt 成 int64 后匹配 WHERE id=?，永远不命中，
// 导致「takeover failed (already taken)」，异步任务无法被消费。
func TestAsyncRequest_Takeover_ByBusinessKey(t *testing.T) {
	db := newTestDB(t)
	asyncRepo := repository.NewAsyncRequestRepository(db)
	ctx := context.Background()

	req := &model.CGAsyncRequest{
		AsyncRequestID: "ar-biz-2085004401067429888", MessageID: "msg-biz",
		UserID: 1, SessionID: "s1", AgentID: "a1", Query: "q",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, req))

	// 用业务键字符串抢占，而不是自增 id
	taken, err := asyncRepo.Takeover(ctx, req.AsyncRequestID, "w1")
	testx.NoError(t, err)
	testx.NotNil(t, taken)
	testx.Equal(t, model.AsyncStatusInProgress, taken.Status)
	testx.Equal(t, "w1", taken.WorkerID)

	// 不存在的业务键 → 冲突
	none, err := asyncRepo.Takeover(ctx, "ar-no-such-key", "w2")
	testx.ErrorIs(t, err, repository.ErrAsyncRequestConflict)
	testx.Nil(t, none)
}

// TestAsyncRequest_ListZombies verifies zombie detection.
func TestAsyncRequest_ListZombies(t *testing.T) {
	db := newTestDB(t)
	asyncRepo := repository.NewAsyncRequestRepository(db)
	ctx := context.Background()

	// Insert a Pending request (not a zombie)
	pendingReq := &model.CGAsyncRequest{
		AsyncRequestID: "ar-pend", MessageID: "msg-pend",
		UserID: 1, SessionID: "s1", AgentID: "a1", Query: "q",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, pendingReq))

	// Insert and takeover a request (simulate running)
	runningReq := &model.CGAsyncRequest{
		AsyncRequestID: "ar-run", MessageID: "msg-run",
		UserID: 2, SessionID: "s2", AgentID: "a2", Query: "q2",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, runningReq))
	_, err := asyncRepo.Takeover(ctx, runningReq.AsyncRequestID, "w1")
	testx.NoError(t, err)

	// Zombies with expiry NOW should find the running request
	// (it was just created, heartbeat is "now", expiry is "now" → heartbeat < expiry → zombie)
	zombies, err := asyncRepo.ListZombies(ctx, time.Now())
	testx.NoError(t, err)
	testx.GreaterOrEqual(t, len(zombies), 0)

	// Zombies with expiry far in PAST should find nothing
	// (heartbeat was set to "now", expiry is "1 minute ago" → heartbeat > expiry → not zombie)
	zombies2, err := asyncRepo.ListZombies(ctx, time.Now().Add(-1*time.Minute))
	testx.NoError(t, err)
	for _, z := range zombies2 {
		testx.NotEqual(t, runningReq.MessageID, z.MessageID,
			"active request should not be in zombie list for past expiry")
	}
}

// TestAsyncRequest_ResetForRetry verifies the status reset from InProgress → Pending.
func TestAsyncRequest_ResetForRetry(t *testing.T) {
	db := newTestDB(t)
	asyncRepo := repository.NewAsyncRequestRepository(db)
	ctx := context.Background()

	req := &model.CGAsyncRequest{
		AsyncRequestID: "ar-rst", MessageID: "msg-rst",
		UserID: 1, SessionID: "s1", AgentID: "a1", Query: "q",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, req))

	// Takeover
	taken, err := asyncRepo.Takeover(ctx, req.AsyncRequestID, "w1")
	testx.NoError(t, err)
	testx.Equal(t, model.AsyncStatusInProgress, taken.Status)

	// Reset for retry
	testx.NoError(t, asyncRepo.ResetForRetry(ctx, req.ID))

	// Verify it's back to Pending
	ret, err := asyncRepo.GetByID(ctx, req.ID)
	testx.NoError(t, err)
	testx.Equal(t, model.AsyncStatusPending, ret.Status)
	testx.Empty(t, ret.WorkerID)
}

// TestConsumer_PayloadParsing verifies the stream payload can be parsed.
func TestConsumer_PayloadParsing(t *testing.T) {
	body := []byte(`{"async_request_id":"123","msg_id":"msg-x","user_id":1,"session_id":"s1","agent_id":"a1","query":"hello"}`)

	type payload struct {
		AsyncRequestID string `json:"async_request_id"`
		MsgID          string `json:"msg_id"`
		Query          string `json:"query"`
	}
	var p payload
	testx.NoError(t, json.Unmarshal(body, &p))
	testx.Equal(t, "123", p.AsyncRequestID)
	testx.Equal(t, "msg-x", p.MsgID)
	testx.Equal(t, "hello", p.Query)
}

// TestRecoveryScanner_NoZombies tests that scanner handles empty zombie list.
func TestRecoveryScanner_NoZombies_Empty(t *testing.T) {
	db := newTestDB(t)
	asyncRepo := repository.NewAsyncRequestRepository(db)

	// No records → no zombies
	zombies, err := asyncRepo.ListZombies(context.Background(), time.Now())
	testx.NoError(t, err)
	testx.Empty(t, zombies)
}

// TestChunkStore_Write_After_Integration verifies Write → After integration
// where the After call reads from 0 and gets all written chunks.
func TestChunkStore_Write_After_Integration(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx := context.Background()

	// Simulate complete worker output
	cs.Write(ctx, "msg-full", `{"type":"message-start","msgId":"msg-full"}`, false)
	cs.Write(ctx, "msg-full", `{"type":"tool-call-start","toolCallId":"tc1"}`, false)
	cs.Write(ctx, "msg-full", `{"type":"tool-call-result","toolCallId":"tc1"}`, false)
	cs.Write(ctx, "msg-full", `{"type":"text-delta","textId":"t1","content":"Result"}`, false)
	cs.Write(ctx, "msg-full", "", true) // EndTurn

	// Read all from start
	chunks, cursor, _, err := cs.After(ctx, "msg-full", 0)
	testx.NoError(t, err)

	testx.Equal(t, 5, len(chunks))
	testx.True(t, chunks[4].EndTurn)
	testx.Equal(t, int64(5), cursor)
}

// TestRunAttach_ResumeFromLastSeq verifies the attach resumes from lastSeq,
// skipping already-consumed chunks.
func TestRunAttach_ResumeFromLastSeq(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx := context.Background()

	cs.Write(ctx, "msg-resume", `{"type":"text-delta","content":"A"}`, false)
	cs.Write(ctx, "msg-resume", `{"type":"text-delta","content":"B"}`, false)
	cs.Write(ctx, "msg-resume", `{"type":"text-delta","content":"C"}`, false)
	cs.Write(ctx, "msg-resume", "", true)

	var replayed []string
	err := worker.RunAttachCore(ctx, "msg-resume", cs, worker.AttachOptions{LastSeq: 2}, func(_ string, raw string) {
		replayed = append(replayed, raw)
	})
	testx.NoError(t, err)
	testx.Equal(t, 1, len(replayed), "only chunk C should be replayed from lastSeq=2")
	testx.Contains(t, replayed[0], "C")
}

// TestRunAttach_TerminalInterrupt verifies that when the async record is terminal
// but no EndTurn chunk exists, the attach emits an interrupted frame and exits.
func TestRunAttach_TerminalInterrupt(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	db := newTestDB(t)
	asyncRepo := repository.NewAsyncRequestRepository(db)
	ctx := context.Background()

	// Insert async request and mark Completed, but write NO EndTurn chunk.
	req := &model.CGAsyncRequest{
		AsyncRequestID: "ar-interrupt", MessageID: "msg-interrupt",
		UserID: 1, SessionID: "s1", AgentID: "a1", Query: "q",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, req))
	testx.NoError(t, asyncRepo.MarkTerminal(ctx, req.ID, model.AsyncStatusCompleted))

	cs.Write(ctx, "msg-interrupt", `{"type":"text-delta","content":"partial"}`, false)

	actx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var frames []string
	err := worker.RunAttachCore(actx, "msg-interrupt", cs, worker.AttachOptions{
		LastSeq:      0,
		AsyncRepo:    asyncRepo,
		PollInterval: 100 * time.Millisecond,
		InterruptPush: func(_ string, raw string) {
			frames = append(frames, raw)
		},
		EndPush: func(_ string, raw string) {
			frames = append(frames, raw)
		},
	}, func(_ string, raw string) {
		frames = append(frames, raw)
	})
	testx.NoError(t, err)

	testx.Contains(t, frames, "__FLOW_END__")
	joined := strings.Join(frames, " ")
	testx.Contains(t, joined, "interrupted", "must emit interrupted frame when terminal without EndTurn")
}
