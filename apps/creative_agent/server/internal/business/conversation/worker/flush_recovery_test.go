package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/worker"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestRebuildChatTextFromChunks verifies the idempotent write-back of chat_text rebuilt from Redis chunks:
// the normal path (with EndTurn) rebuilds the ChatResponse, and repeated calls yield the same result.
func TestRebuildChatTextFromChunks(t *testing.T) {
	db := newTestDB(t)
	// The test needs the message_record table (ReadModifyWriteInTx depends on it)
	testx.NoError(t, db.AutoMigrate(&model.CGMessageRecord{}))

	cs := infra.NewMemoryChunkStore()
	msgRepo := repository.NewMessageRecordRepository(db)
	ctx := context.Background()

	msgID := "msg-rebuild"
	// con_reader pre-creates the record (QueryInfo present, ChatResponse empty)
	ct := model.NewChatText("用户问题")
	ctJSON, _ := json.Marshal(ct)
	testx.NoError(t, msgRepo.Insert(ctx, &model.CGMessageRecord{
		MessageID: msgID, UserID: 1, AgentID: "a1", SessionID: "s1",
		ChatText: string(ctJSON), Status: model.MessageStatusInProgress,
	}))

	// Worker output: two frames + EndTurn
	testx.NoError(t, cs.Write(ctx, msgID, `{"type":"text-delta","textId":"t1","content":"你好"}`, false))
	testx.NoError(t, cs.Write(ctx, msgID, `{"type":"text-delta","textId":"t1","content":"世界"}`, false))
	testx.NoError(t, cs.Write(ctx, msgID, "", true))

	testx.NoError(t, worker.RebuildChatTextFromChunks(ctx, msgRepo, cs, msgID))

	rec, err := msgRepo.GetByMessageID(ctx, msgID)
	testx.NoError(t, err)
	testx.Equal(t, model.MessageStatusNormal, rec.Status)

	parsed, err := rec.ParseChatText()
	testx.NoError(t, err)
	testx.Len(t, parsed.ChatResponse, 2, "two frames should rebuild two ChatResponseItems")
	testx.Equal(t, int64(1), parsed.ChatResponse[0].SendID)
	// Message stores the complete SSE frame JSON (rebuild semantics = restore the worker's frames in order)
	testx.Equal(t, `{"type":"text-delta","textId":"t1","content":"你好"}`, parsed.ChatResponse[0].Message)
	testx.False(t, parsed.ChatResponse[0].EndTurn)
	testx.Equal(t, int64(2), parsed.ChatResponse[1].SendID)
	testx.True(t, parsed.ChatResponse[1].EndTurn, "the last item should be marked EndTurn")

	// Idempotent: repeated rebuilds match
	testx.NoError(t, worker.RebuildChatTextFromChunks(ctx, msgRepo, cs, msgID))
	rec2, _ := msgRepo.GetByMessageID(ctx, msgID)
	testx.Equal(t, rec.ChatText, rec2.ChatText, "repeated rebuilds should be idempotent")
}

// TestRebuildChatTextFromChunks_ChunkExpired verifies that ErrChunkExpired is returned when chunks expired (no EndTurn and 0 frames read).
func TestRebuildChatTextFromChunks_ChunkExpired(t *testing.T) {
	db := newTestDB(t)
	testx.NoError(t, db.AutoMigrate(&model.CGMessageRecord{}))

	cs := infra.NewMemoryChunkStore()
	msgRepo := repository.NewMessageRecordRepository(db)
	ctx := context.Background()

	msgID := "msg-expired"
	ct := model.NewChatText("q")
	ctJSON, _ := json.Marshal(ct)
	testx.NoError(t, msgRepo.Insert(ctx, &model.CGMessageRecord{
		MessageID: msgID, UserID: 1, AgentID: "a1", SessionID: "s1",
		ChatText: string(ctJSON), Status: model.MessageStatusInProgress,
	}))

	// No chunks → After returns empty and the cursor does not move → ErrChunkExpired
	err := worker.RebuildChatTextFromChunks(ctx, msgRepo, cs, msgID)
	testx.Error(t, err)
	testx.True(t, errors.Is(err, worker.ErrChunkExpired), "ErrChunkExpired should be returned, got: %v", err)
}

// TestRecovery_StrategyA_RebuildAndComplete verifies recovery strategy A when EndTurn exists:
// rebuild chat_text from chunks, write it back to message_record, then flip to Completed.
func TestRecovery_StrategyA_RebuildAndComplete(t *testing.T) {
	db := newTestDB(t)
	testx.NoError(t, db.AutoMigrate(&model.CGMessageRecord{}))

	asyncRepo := repository.NewAsyncRequestRepository(db)
	msgRepo := repository.NewMessageRecordRepository(db)
	chunkStore := infra.NewMemoryChunkStore()
	streamQ := infra.NewMemoryStreamQueue("cg:stream:test", "cg:workers", "rec")
	ctx := context.Background()

	// Pre-create async_request + message_record, simulating a zombie left after the worker's persistence failed
	req := &model.CGAsyncRequest{
		AsyncRequestID: "ar-zombie", MessageID: "msg-zombie",
		UserID: 1, SessionID: "s1", AgentID: "a1", Query: "q",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, req))
	// Simulate claimed by a worker (InProgress) but persistence failed and the heartbeat expired
	_, err := asyncRepo.Takeover(ctx, req.AsyncRequestID, "w-dead")
	testx.NoError(t, err)

	ct := model.NewChatText("q")
	ctJSON, _ := json.Marshal(ct)
	testx.NoError(t, msgRepo.Insert(ctx, &model.CGMessageRecord{
		MessageID: "msg-zombie", UserID: 1, AgentID: "a1", SessionID: "s1",
		ChatText: string(ctJSON), Status: model.MessageStatusInProgress,
	}))

	// Chunks hold the complete output + EndTurn (ReAct finished; only persistence is missing)
	testx.NoError(t, chunkStore.Write(ctx, "msg-zombie", `{"type":"text-delta","textId":"t1","content":"重建内容"}`, false))
	testx.NoError(t, chunkStore.Write(ctx, "msg-zombie", "", true))

	// Expire the heartbeat (the criterion recovery scans for)
	testx.NoError(t, db.Model(&model.CGAsyncRequest{}).
		Where("id = ?", req.ID).
		Update("heartbeat_at", 1).Error)

	// Run one scan round
	recovery := worker.NewRecoveryScanner(worker.RecoveryConfig{
		AsyncRepo:  asyncRepo,
		MsgRepo:    msgRepo,
		ChunkStore: chunkStore,
		StreamQ:    streamQ,
		Logger:     testLogger(),
	})
	recovery.ScanOnce(ctx)

	// Status flipped to Completed
	ret, err := asyncRepo.GetByID(ctx, req.ID)
	testx.NoError(t, err)
	testx.Equal(t, model.AsyncStatusCompleted, ret.Status)

	// chat_text was rebuilt from chunks (no longer an empty ChatResponse)
	rec, err := msgRepo.GetByMessageID(ctx, "msg-zombie")
	testx.NoError(t, err)
	parsed, err := rec.ParseChatText()
	testx.NoError(t, err)
	testx.Len(t, parsed.ChatResponse, 1)
	testx.Equal(t, `{"type":"text-delta","textId":"t1","content":"重建内容"}`, parsed.ChatResponse[0].Message)
	testx.Equal(t, model.MessageStatusNormal, rec.Status)
}

// failMsgRepo is a MessageRecordRepository stub that always errors, used to verify the terminal semantics when recovery strategy A's rebuild fails.
type failMsgRepo struct{}

func (failMsgRepo) Insert(ctx context.Context, m *model.CGMessageRecord) error {
	return errors.New("injected: insert fail")
}
func (failMsgRepo) GetByMessageID(ctx context.Context, messageID string) (*model.CGMessageRecord, error) {
	return nil, errors.New("injected: get fail")
}
func (failMsgRepo) UpdateByMessageID(ctx context.Context, messageID string, assigns map[string]any) error {
	return errors.New("injected: update fail")
}
func (failMsgRepo) ReadModifyWriteInTx(ctx context.Context, messageID string, fn func(*model.CGMessageRecord) (map[string]any, error)) error {
	return errors.New("injected: tx fail")
}

// TestRecovery_StrategyA_RebuildFailed_MarksFailed verifies that when strategy A's rebuild persistence fails:
// it marks Failed (terminal close-out) instead of Completed, avoiding a dirty "completed but empty" record.
func TestRecovery_StrategyA_RebuildFailed_MarksFailed(t *testing.T) {
	db := newTestDB(t)
	testx.NoError(t, db.AutoMigrate(&model.CGMessageRecord{}))

	asyncRepo := repository.NewAsyncRequestRepository(db)
	chunkStore := infra.NewMemoryChunkStore()
	streamQ := infra.NewMemoryStreamQueue("cg:stream:test", "cg:workers", "rec")
	ctx := context.Background()

	req := &model.CGAsyncRequest{
		AsyncRequestID: "ar-zombie-fail", MessageID: "msg-zombie-fail",
		UserID: 1, SessionID: "s1", AgentID: "a1", Query: "q",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, req))
	_, err := asyncRepo.Takeover(ctx, req.AsyncRequestID, "w-dead")
	testx.NoError(t, err)

	// Chunks have EndTurn (ReAct finished)
	testx.NoError(t, chunkStore.Write(ctx, "msg-zombie-fail", `{"type":"text-delta","content":"x"}`, false))
	testx.NoError(t, chunkStore.Write(ctx, "msg-zombie-fail", "", true))
	testx.NoError(t, db.Model(&model.CGAsyncRequest{}).
		Where("id = ?", req.ID).
		Update("heartbeat_at", 1).Error)

	// Inject the failing msgRepo stub → the rebuild must fail
	recovery := worker.NewRecoveryScanner(worker.RecoveryConfig{
		AsyncRepo:  asyncRepo,
		MsgRepo:    failMsgRepo{},
		ChunkStore: chunkStore,
		StreamQ:    streamQ,
		Logger:     testLogger(),
	})
	recovery.ScanOnce(ctx)

	// The terminal state is Failed, not Completed
	ret, err := asyncRepo.GetByID(ctx, req.ID)
	testx.NoError(t, err)
	testx.Equal(t, model.AsyncStatusFailed, ret.Status)
}

// TestRecovery_StrategyA_NoMsgRepo_LegacyFallback verifies that when msgRepo is not wired (legacy config):
// strategy A degrades to flipping Completed only (no rebuild), keeping backward compatibility.
func TestRecovery_StrategyA_NoMsgRepo_LegacyFallback(t *testing.T) {
	db := newTestDB(t)
	testx.NoError(t, db.AutoMigrate(&model.CGMessageRecord{}))

	asyncRepo := repository.NewAsyncRequestRepository(db)
	chunkStore := infra.NewMemoryChunkStore()
	streamQ := infra.NewMemoryStreamQueue("cg:stream:test", "cg:workers", "rec")
	ctx := context.Background()

	req := &model.CGAsyncRequest{
		AsyncRequestID: "ar-zombie-legacy", MessageID: "msg-zombie-legacy",
		UserID: 1, SessionID: "s1", AgentID: "a1", Query: "q",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, req))
	_, err := asyncRepo.Takeover(ctx, req.AsyncRequestID, "w-dead")
	testx.NoError(t, err)
	testx.NoError(t, chunkStore.Write(ctx, "msg-zombie-legacy", `{"type":"text-delta","content":"x"}`, false))
	testx.NoError(t, chunkStore.Write(ctx, "msg-zombie-legacy", "", true))
	testx.NoError(t, db.Model(&model.CGAsyncRequest{}).
		Where("id = ?", req.ID).
		Update("heartbeat_at", 1).Error)

	// Do not inject MsgRepo (zero value)
	recovery := worker.NewRecoveryScanner(worker.RecoveryConfig{
		AsyncRepo:  asyncRepo,
		ChunkStore: chunkStore,
		StreamQ:    streamQ,
		Logger:     testLogger(),
	})
	recovery.ScanOnce(ctx)

	ret, err := asyncRepo.GetByID(ctx, req.ID)
	testx.NoError(t, err)
	testx.Equal(t, model.AsyncStatusCompleted, ret.Status, "without msgRepo it degrades to the legacy behavior (state flip only)")
}
