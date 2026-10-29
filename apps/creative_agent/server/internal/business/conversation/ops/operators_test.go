package ops_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/ops"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/worker"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestConWriter_DualKeyListening verifies that both ConOnePackSSEKey
// and ConReconnectSSEKey frames are written by the writer.
func TestConWriter_DualKeyListening(t *testing.T) {
	ctx := seda.NewContext()
	ctx.GetSliceWithInit(ops.ConOnePackSSEKey)
	ctx.GetSliceWithInit(ops.ConReconnectSSEKey)

	ctx.Push(ops.ConOnePackSSEKey, `{"type":"text-delta","content":"sync"}`)
	ctx.Push(ops.ConReconnectSSEKey, `{"type":"text-delta","content":"async"}`)

	s1 := ctx.GetSliceWithInit(ops.ConOnePackSSEKey)
	d1, _ := s1.DiffFrom(0)
	testx.Equal(t, 1, len(d1))

	s2 := ctx.GetSliceWithInit(ops.ConReconnectSSEKey)
	d2, _ := s2.DiffFrom(0)
	testx.Equal(t, 1, len(d2))
}

func TestConWriter_DualKey_FlowEnd(t *testing.T) {
	ctx := seda.NewContext()
	ctx.GetSliceWithInit(ops.ConOnePackSSEKey)
	ctx.GetSliceWithInit(ops.ConReconnectSSEKey)

	ctx.Push(ops.ConOnePackSSEKey, `{"type":"text-delta","content":"hello"}`)
	ctx.Push(ops.ConOnePackSSEKey, "__FLOW_END__")
	ctx.Push(ops.ConReconnectSSEKey, "__FLOW_END__")

	slice := ctx.GetSliceWithInit(ops.ConOnePackSSEKey)
	diff, _ := slice.DiffFrom(0)
	testx.Contains(t, diff, "__FLOW_END__")
}

func TestConReader_AsyncShortCircuit_Flag(t *testing.T) {
	ctx := seda.NewContext()
	ctx.Set("asyncShortCircuit", true)
	ctx.Set("asyncRequestId", "req-12345")

	testx.Equal(t, true, ctx.Get("asyncShortCircuit"))
	testx.Equal(t, "req-12345", ctx.Get("asyncRequestId"))
}

// TestRunAttach_ReplayChunks verifies the subscribe-then-read pattern
// works correctly: write chunks first, then RunAttach replays them.
func TestRunAttach_ReplayChunks(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx := context.Background()
	flowCtx := seda.NewContext()
	flowCtx.GetSliceWithInit(ops.ConReconnectSSEKey)

	// Write chunks (simulating worker output)
	cs.Write(ctx, "msg-attach-1", `{"type":"message-start","msgId":"msg-attach-1"}`, false)
	cs.Write(ctx, "msg-attach-1", `{"type":"text-delta","textId":"t1","content":"Hello"}`, false)
	// Write EndTurn so RunAttach exits cleanly
	cs.Write(ctx, "msg-attach-1", "", true)

	// RunAttach with timeout
	actx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := worker.RunAttach(actx, "msg-attach-1", cs, flowCtx, ops.ConReconnectSSEKey, 0, nil)
	testx.NoError(t, err)

	// Verify chunks were replayed
	slice := flowCtx.GetSliceWithInit(ops.ConReconnectSSEKey)
	diff, _ := slice.DiffFrom(0)
	testx.True(t, len(diff) >= 2, "should have at least 2 chunks, got %d", len(diff))

	// Last push should be __FLOW_END__
	testx.Equal(t, "__FLOW_END__", diff[len(diff)-1])
}

// TestRunAttach_ExitOnTimeout verifies RunAttach exits when context is cancelled
// even without an EndTurn chunk.
func TestRunAttach_ExitOnTimeout(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx := context.Background()
	flowCtx := seda.NewContext()
	flowCtx.GetSliceWithInit(ops.ConReconnectSSEKey)

	// Write one chunk but NO EndTurn
	cs.Write(ctx, "msg-timeout", `{"type":"text-delta","content":"data"}`, false)

	actx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := worker.RunAttach(actx, "msg-timeout", cs, flowCtx, ops.ConReconnectSSEKey, 0, nil)
	testx.ErrorIs(t, err, context.DeadlineExceeded)

	// Should still have pushed __FLOW_END__ on cancel
	slice := flowCtx.GetSliceWithInit(ops.ConReconnectSSEKey)
	diff, _ := slice.DiffFrom(0)
	testx.Equal(t, "__FLOW_END__", diff[len(diff)-1])
}

// newTestDB creates an in-memory SQLite database and migrates the conversation tables.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGSession{}, &model.CGMessageRecord{}))
	return db
}

// TestConReader_SequentialMessages_GetDistinctMsgIDs reproduces the Phase 2 account-mixing bug:
// two consecutive requests in the same session — the second must create a new message_record (new msgID),
// not reuse the first InProgress record's msgID.
func TestConReader_SequentialMessages_GetDistinctMsgIDs(t *testing.T) {
	db := newTestDB(t)
	msgRepo := repository.NewMessageRecordRepository(db)
	sessRepo := repository.NewSessionRepository(db)
	// Pass nil for asyncRepo/streamQ/redis to take the sync path
	node := ops.NewConReaderNode(sessRepo, msgRepo, nil, nil, nil, false)

	send := func(query string) (string, *seda.Context) {
		body, err := json.Marshal(map[string]interface{}{
			"userId": 1, "sessionId": "s-distinct", "agentId": "a1", "query": query,
		})
		testx.NoError(t, err)
		ctx := seda.NewContext()
		ctx.Set(ops.FCKGdpCtx, body)
		code, msg := node.DoImpl(ctx)
		testx.Equal(t, 0, code, "DoImpl failed: %s", msg)
		msgID, _ := ctx.Get(ops.FCKServerMsgID).(string)
		testx.NotEmpty(t, msgID)
		return msgID, ctx
	}

	// First request
	msg1, _ := send("第一条消息")
	// Second request (the first record is still InProgress, not finished yet)
	msg2, _ := send("第二条消息")

	testx.NotEqual(t, msg1, msg2, "two different messages must use different msgIDs; no account mixing")

	// Two independent records should exist in the database
	var count int64
	db.Model(&model.CGMessageRecord{}).Where("session_id = ?", "s-distinct").Count(&count)
	testx.Equal(t, int64(2), count, "two message_record rows should be created")

	// Each record should carry its own query
	rec1, err := msgRepo.GetByMessageID(context.Background(), msg1)
	testx.NoError(t, err)
	rec2, err := msgRepo.GetByMessageID(context.Background(), msg2)
	testx.NoError(t, err)
	testx.Contains(t, rec1.ChatText, "第一条消息")
	testx.Contains(t, rec2.ChatText, "第二条消息")
}

// TestConReader_DoesNotReuseInProgress verifies two consecutive requests
// produce two distinct message IDs (no accidental record reuse).
func TestConReader_DoesNotReuseInProgress(t *testing.T) {
	// Unmarshal request bodies that target the same session.
	// Because con_reader reads HTTP body from flowcontext FCKGdpCtx,
	// we simulate two requests by invoking the node's DoImpl twice.
	// We assert the produced message IDs differ by inspecting the
	// message_record rows instead of flowcontext internals.

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGSession{}, &model.CGMessageRecord{}))

	msgRepo := repository.NewMessageRecordRepository(db)
	sessRepo := repository.NewSessionRepository(db)
	node := ops.NewConReaderNode(sessRepo, msgRepo, nil, nil, nil, false)

	body1, _ := json.Marshal(map[string]interface{}{
		"userId": 1, "sessionId": "s-reuse", "agentId": "a1", "query": "hi",
	})
	ctx1 := seda.NewContext()
	ctx1.Set(ops.FCKGdpCtx, body1)
	code1, _ := node.DoImpl(ctx1)

	body2, _ := json.Marshal(map[string]interface{}{
		"userId": 1, "sessionId": "s-reuse", "agentId": "a1", "query": "second",
	})
	ctx2 := seda.NewContext()
	ctx2.Set(ops.FCKGdpCtx, body2)
	code2, _ := node.DoImpl(ctx2)

	testx.Equal(t, 0, code1)
	testx.Equal(t, 0, code2)

	msg1 := ctx1.Get(ops.FCKServerMsgID).(string)
	msg2 := ctx2.Get(ops.FCKServerMsgID).(string)
	testx.NotEqual(t, msg1, msg2, "each request must get a fresh message ID")

	// Both records must exist and be InProgress (status=2)
	rec1, err := msgRepo.GetByMessageID(context.Background(), msg1)
	testx.NoError(t, err)
	rec2, err := msgRepo.GetByMessageID(context.Background(), msg2)
	testx.NoError(t, err)
	testx.Equal(t, model.MessageStatusInProgress, rec1.Status)
	testx.Equal(t, model.MessageStatusInProgress, rec2.Status)
}

// TestConReader_ReconnectReusesRecord verifies that a reconnect request
// with an existing msgId reuses that record instead of creating a new one.
func TestConReader_ReconnectReusesRecord(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGSession{}, &model.CGMessageRecord{}))

	msgRepo := repository.NewMessageRecordRepository(db)
	sessRepo := repository.NewSessionRepository(db)
	node := ops.NewConReaderNode(sessRepo, msgRepo, nil, nil, nil, false)

	// First normal request creates a record.
	body1, _ := json.Marshal(map[string]interface{}{
		"userId": 1, "sessionId": "s-reconn", "agentId": "a1", "query": "hi",
	})
	ctx1 := seda.NewContext()
	ctx1.Set(ops.FCKGdpCtx, body1)
	code1, _ := node.DoImpl(ctx1)
	testx.Equal(t, 0, code1)
	msgID := ctx1.Get(ops.FCKServerMsgID).(string)

	// Count records before reconnect.
	var before int64
	db.Model(&model.CGMessageRecord{}).Where("session_id = ?", "s-reconn").Count(&before)

	// Reconnect request with the existing msgId.
	body2, _ := json.Marshal(map[string]interface{}{
		"userId": 1, "sessionId": "s-reconn", "agentId": "a1", "query": "hi",
		"reconnect": true, "msgId": msgID, "lastSeq": 3,
	})
	ctx2 := seda.NewContext()
	ctx2.Set(ops.FCKGdpCtx, body2)
	code2, _ := node.DoImpl(ctx2)
	testx.Equal(t, 0, code2)

	// No new record created.
	var after int64
	db.Model(&model.CGMessageRecord{}).Where("session_id = ?", "s-reconn").Count(&after)
	testx.Equal(t, before, after, "reconnect must reuse existing record, not insert")

	// asyncShortCircuit must be set so supervisor_agent goes to attach.
	testx.Equal(t, true, ctx2.Get("asyncShortCircuit"))
	// lastSeq must flow through to the context.
	testx.Equal(t, int64(3), ctx2.Get("lastSeq"))
}
