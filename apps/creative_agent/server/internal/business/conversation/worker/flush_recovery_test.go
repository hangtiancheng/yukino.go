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

// TestRebuildChatTextFromChunks 验证从 Redis chunk 重建 chat_text 的幂等写回：
// 正常路径（含 EndTurn）重建出 ChatResponse，且重复调用结果一致。
func TestRebuildChatTextFromChunks(t *testing.T) {
	db := newTestDB(t)
	// 测试需要 message_record 表（ReadModifyWriteInTx 依赖）
	testx.NoError(t, db.AutoMigrate(&model.CGMessageRecord{}))

	cs := infra.NewMemoryChunkStore()
	msgRepo := repository.NewMessageRecordRepository(db)
	ctx := context.Background()

	msgID := "msg-rebuild"
	// con_reader 预建记录（QueryInfo 已有，ChatResponse 空）
	ct := model.NewChatText("用户问题")
	ctJSON, _ := json.Marshal(ct)
	testx.NoError(t, msgRepo.Insert(ctx, &model.CGMessageRecord{
		MessageID: msgID, UserID: 1, AgentID: "a1", SessionID: "s1",
		ChatText: string(ctJSON), Status: model.MessageStatusInProgress,
	}))

	// Worker 产出：两帧 + EndTurn
	testx.NoError(t, cs.Write(ctx, msgID, `{"type":"text-delta","textId":"t1","content":"你好"}`, false))
	testx.NoError(t, cs.Write(ctx, msgID, `{"type":"text-delta","textId":"t1","content":"世界"}`, false))
	testx.NoError(t, cs.Write(ctx, msgID, "", true))

	testx.NoError(t, worker.RebuildChatTextFromChunks(ctx, msgRepo, cs, msgID))

	rec, err := msgRepo.GetByMessageID(ctx, msgID)
	testx.NoError(t, err)
	testx.Equal(t, model.MessageStatusNormal, rec.Status)

	parsed, err := rec.ParseChatText()
	testx.NoError(t, err)
	testx.Len(t, parsed.ChatResponse, 2, "两帧应重建出两条 ChatResponseItem")
	testx.Equal(t, int64(1), parsed.ChatResponse[0].SendID)
	// Message 里存的是完整 SSE 帧 JSON（重建语义 = 按序还原 worker 产出的帧）
	testx.Equal(t, `{"type":"text-delta","textId":"t1","content":"你好"}`, parsed.ChatResponse[0].Message)
	testx.False(t, parsed.ChatResponse[0].EndTurn)
	testx.Equal(t, int64(2), parsed.ChatResponse[1].SendID)
	testx.True(t, parsed.ChatResponse[1].EndTurn, "最后一条标记 EndTurn")

	// 幂等：重复重建结果一致
	testx.NoError(t, worker.RebuildChatTextFromChunks(ctx, msgRepo, cs, msgID))
	rec2, _ := msgRepo.GetByMessageID(ctx, msgID)
	testx.Equal(t, rec.ChatText, rec2.ChatText, "重复重建应幂等")
}

// TestRebuildChatTextFromChunks_ChunkExpired 验证 chunk 已过期（无 EndTurn 且读到 0 帧）时返回 ErrChunkExpired。
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

	// 无 chunk → After 返回空且游标不动 → ErrChunkExpired
	err := worker.RebuildChatTextFromChunks(ctx, msgRepo, cs, msgID)
	testx.Error(t, err)
	testx.True(t, errors.Is(err, worker.ErrChunkExpired), "应返回 ErrChunkExpired，实际: %v", err)
}

// TestRecovery_StrategyA_RebuildAndComplete 验证 recovery 策略 A 在 EndTurn 存在时：
// 从 chunk 重建 chat_text 写回 message_record，再翻 Completed。
func TestRecovery_StrategyA_RebuildAndComplete(t *testing.T) {
	db := newTestDB(t)
	testx.NoError(t, db.AutoMigrate(&model.CGMessageRecord{}))

	asyncRepo := repository.NewAsyncRequestRepository(db)
	msgRepo := repository.NewMessageRecordRepository(db)
	chunkStore := infra.NewMemoryChunkStore()
	streamQ := infra.NewMemoryStreamQueue("cg:stream:test", "cg:workers", "rec")
	ctx := context.Background()

	// 预建 async_request + message_record，模拟 worker 落库失败后留下的僵尸
	req := &model.CGAsyncRequest{
		AsyncRequestID: "ar-zombie", MessageID: "msg-zombie",
		UserID: 1, SessionID: "s1", AgentID: "a1", Query: "q",
	}
	testx.NoError(t, asyncRepo.InsertPending(ctx, req))
	// 模拟已被 worker 抢占（InProgress）但落库失败、心跳过期
	_, err := asyncRepo.Takeover(ctx, req.AsyncRequestID, "w-dead")
	testx.NoError(t, err)

	ct := model.NewChatText("q")
	ctJSON, _ := json.Marshal(ct)
	testx.NoError(t, msgRepo.Insert(ctx, &model.CGMessageRecord{
		MessageID: "msg-zombie", UserID: 1, AgentID: "a1", SessionID: "s1",
		ChatText: string(ctJSON), Status: model.MessageStatusInProgress,
	}))

	// chunk 里有完整产出 + EndTurn（ReAct 已跑完，只差落库）
	testx.NoError(t, chunkStore.Write(ctx, "msg-zombie", `{"type":"text-delta","textId":"t1","content":"重建内容"}`, false))
	testx.NoError(t, chunkStore.Write(ctx, "msg-zombie", "", true))

	// 心跳置为过期（recovery 扫到的判据）
	testx.NoError(t, db.Model(&model.CGAsyncRequest{}).
		Where("id = ?", req.ID).
		Update("heartbeat_at", 1).Error)

	// 跑一轮扫描
	recovery := worker.NewRecoveryScanner(worker.RecoveryConfig{
		AsyncRepo:  asyncRepo,
		MsgRepo:    msgRepo,
		ChunkStore: chunkStore,
		StreamQ:    streamQ,
		Logger:     testLogger(),
	})
	recovery.ScanOnce(ctx)

	// 状态翻 Completed
	ret, err := asyncRepo.GetByID(ctx, req.ID)
	testx.NoError(t, err)
	testx.Equal(t, model.AsyncStatusCompleted, ret.Status)

	// chat_text 已从 chunk 重建（不再是空 ChatResponse）
	rec, err := msgRepo.GetByMessageID(ctx, "msg-zombie")
	testx.NoError(t, err)
	parsed, err := rec.ParseChatText()
	testx.NoError(t, err)
	testx.Len(t, parsed.ChatResponse, 1)
	testx.Equal(t, `{"type":"text-delta","textId":"t1","content":"重建内容"}`, parsed.ChatResponse[0].Message)
	testx.Equal(t, model.MessageStatusNormal, rec.Status)
}

// failMsgRepo 是总是返回错误的 MessageRecordRepository 桩，用于验证 recovery 策略 A 重建失败时的终态语义。
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

// TestRecovery_StrategyA_RebuildFailed_MarksFailed 验证策略 A 重建落库失败时：
// 标 Failed（终态收口）而不是 Completed，避免留下"已完成但无内容"的脏记录。
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

	// chunk 有 EndTurn（ReAct 已跑完）
	testx.NoError(t, chunkStore.Write(ctx, "msg-zombie-fail", `{"type":"text-delta","content":"x"}`, false))
	testx.NoError(t, chunkStore.Write(ctx, "msg-zombie-fail", "", true))
	testx.NoError(t, db.Model(&model.CGAsyncRequest{}).
		Where("id = ?", req.ID).
		Update("heartbeat_at", 1).Error)

	// msgRepo 注入失败桩 → 重建必然失败
	recovery := worker.NewRecoveryScanner(worker.RecoveryConfig{
		AsyncRepo:  asyncRepo,
		MsgRepo:    failMsgRepo{},
		ChunkStore: chunkStore,
		StreamQ:    streamQ,
		Logger:     testLogger(),
	})
	recovery.ScanOnce(ctx)

	// 终态为 Failed，而不是 Completed
	ret, err := asyncRepo.GetByID(ctx, req.ID)
	testx.NoError(t, err)
	testx.Equal(t, model.AsyncStatusFailed, ret.Status)
}

// TestRecovery_StrategyA_NoMsgRepo_LegacyFallback 验证 msgRepo 未装配时（旧配置）：
// 策略 A 退化为仅翻 Completed（不重建），保持向后兼容。
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

	// 不注入 MsgRepo（零值）
	recovery := worker.NewRecoveryScanner(worker.RecoveryConfig{
		AsyncRepo:  asyncRepo,
		ChunkStore: chunkStore,
		StreamQ:    streamQ,
		Logger:     testLogger(),
	})
	recovery.ScanOnce(ctx)

	ret, err := asyncRepo.GetByID(ctx, req.ID)
	testx.NoError(t, err)
	testx.Equal(t, model.AsyncStatusCompleted, ret.Status, "无 msgRepo 时退化为旧行为（仅翻状态）")
}
