package infra_test

import (
	"context"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// ---------- MemoryChunkStore（yukino_cache Group 底座） ----------

func TestChunkStore_Write_After_EndToEnd(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx := context.Background()

	testx.NoError(t, cs.Write(ctx, "msg-1", `{"type":"text-delta","content":"Hello"}`, false))
	testx.NoError(t, cs.Write(ctx, "msg-1", `{"type":"text-delta","content":" World"}`, false))

	chunks, cursor, _, err := cs.After(ctx, "msg-1", 0)
	testx.NoError(t, err)
	testx.Equal(t, 2, len(chunks))
	testx.Equal(t, int64(2), cursor)
}

func TestChunkStore_After_Incremental(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx := context.Background()

	testx.NoError(t, cs.Write(ctx, "msg-2", "chunk-1", false))

	chunks, cursor, _, err := cs.After(ctx, "msg-2", 0)
	testx.NoError(t, err)
	testx.Equal(t, 1, len(chunks))
	testx.Equal(t, "chunk-1", chunks[0].Payload)
	testx.Equal(t, int64(1), cursor)

	testx.NoError(t, cs.Write(ctx, "msg-2", "chunk-2", false))

	chunks2, cursor2, _, err := cs.After(ctx, "msg-2", cursor)
	testx.NoError(t, err)
	testx.Equal(t, 1, len(chunks2))
	testx.Equal(t, "chunk-2", chunks2[0].Payload)
	testx.Equal(t, int64(2), cursor2)
}

func TestChunkStore_Empty(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx := context.Background()

	chunks, cursor, _, err := cs.After(ctx, "msg-empty", 0)
	testx.NoError(t, err)
	testx.Empty(t, chunks)
	testx.Equal(t, int64(0), cursor)
}

func TestChunkStore_Write_EndTurn_Marker(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx := context.Background()

	testx.NoError(t, cs.Write(ctx, "msg-3", `{"type":"text-delta","content":"A"}`, false))

	hasEnd, err := cs.HasEndTurn(ctx, "msg-3")
	testx.NoError(t, err)
	testx.False(t, hasEnd)

	testx.NoError(t, cs.Write(ctx, "msg-3", "", true))

	hasEnd, err = cs.HasEndTurn(ctx, "msg-3")
	testx.NoError(t, err)
	testx.True(t, hasEnd)
}

func TestChunkStore_After_NotificationChannel(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testx.NoError(t, cs.Write(ctx, "msg-4", "initial", false))
	_, _, notify, err := cs.After(ctx, "msg-4", 0)
	testx.NoError(t, err)

	go func() { _ = cs.Write(ctx, "msg-4", "new", false) }()

	select {
	case <-notify:
		// 收到通知
	case <-time.After(3 * time.Second):
		t.Fatal("notification channel did not fire after write")
	}
}

func TestChunkStore_EndTurnChunk_HasEndTurn(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx := context.Background()

	testx.NoError(t, cs.Write(ctx, "msg-et", "", true))

	hasEnd, err := cs.HasEndTurn(ctx, "msg-et")
	testx.NoError(t, err)
	testx.True(t, hasEnd)

	hasEnd2, err := cs.HasEndTurn(ctx, "msg-none")
	testx.NoError(t, err)
	testx.False(t, hasEnd2)
}

func TestChunkStore_ConcurrentAccess(t *testing.T) {
	cs := infra.NewMemoryChunkStore()
	defer cs.Close()
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			_ = cs.Write(ctx, "msg-conc", "chunk", false)
		}
		_ = cs.Write(ctx, "msg-conc", "", true)
		close(done)
	}()

	var cursor int64
	for {
		chunks, newCursor, _, err := cs.After(ctx, "msg-conc", cursor)
		testx.NoError(t, err)
		cursor = newCursor
		for _, c := range chunks {
			if c.EndTurn {
				return
			}
		}
		select {
		case <-done:
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// ---------- MemoryStreamQueue ----------

func TestStreamQueue_Push_Receive_Ack(t *testing.T) {
	sq := infra.NewMemoryStreamQueue("test:stream:1", "test-group", "consumer-1")
	ctx := context.Background()

	testx.NoError(t, sq.Push(ctx, "msg-101", []byte(`{"query":"hello"}`)))

	msg, err := sq.Receive(ctx)
	testx.NoError(t, err)
	testx.NotNil(t, msg)
	testx.Equal(t, "msg-101", msg.MsgID)
	testx.Equal(t, `{"query":"hello"}`, string(msg.Body))

	testx.NoError(t, sq.Ack(ctx, msg.ID))
}

func TestStreamQueue_Receive_Empty(t *testing.T) {
	sq := infra.NewMemoryStreamQueue("test:stream:empty", "g2", "c2")

	// 空队列 + 短超时：返回 nil 消息（超时）或 ctx 错误，均视为"无消息"。
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	msg, err := sq.Receive(ctx)
	if err == nil {
		testx.Nil(t, msg)
	}
}

func TestStreamQueue_Requeue(t *testing.T) {
	sq := infra.NewMemoryStreamQueue("test:stream:rq", "g3", "c3")
	ctx := context.Background()

	testx.NoError(t, sq.Requeue(ctx, "msg-req", []byte(`{"query":"retry"}`)))

	msg, err := sq.Receive(ctx)
	testx.NoError(t, err)
	testx.NotNil(t, msg)
	testx.Equal(t, "msg-req", msg.MsgID)
	_ = sq.Ack(ctx, msg.ID)
}

func TestStreamQueue_MultipleMessages(t *testing.T) {
	sq := infra.NewMemoryStreamQueue("test:stream:multi", "g4", "c4")
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		testx.NoError(t, sq.Push(ctx, "msg-multi-"+string(rune('0'+i)), []byte(`{"idx":`+string(rune('0'+i))+`}`)))
	}

	for i := 0; i < 5; i++ {
		msg, err := sq.Receive(ctx)
		testx.NoError(t, err)
		testx.NotNil(t, msg)
		testx.NoError(t, sq.Ack(ctx, msg.ID))
	}
}

// ---------- MemoryLocker ----------

func TestMemoryLocker_SetNX_Exclusive(t *testing.T) {
	locker := infra.NewMemoryLocker()
	ctx := context.Background()

	ok, err := locker.SetNX(ctx, "lock-1", "owner-a", time.Minute)
	testx.NoError(t, err)
	testx.True(t, ok, "首次 SetNX 应抢到锁")

	ok2, err := locker.SetNX(ctx, "lock-1", "owner-b", time.Minute)
	testx.NoError(t, err)
	testx.False(t, ok2, "锁未释放时第二次 SetNX 应失败")

	testx.NoError(t, locker.Del(ctx, "lock-1"))

	ok3, err := locker.SetNX(ctx, "lock-1", "owner-b", time.Minute)
	testx.NoError(t, err)
	testx.True(t, ok3, "Del 后应可重新抢锁")
}

func TestMemoryLocker_ExpireAndTTL(t *testing.T) {
	locker := infra.NewMemoryLocker()
	ctx := context.Background()

	ok, err := locker.SetNX(ctx, "lock-ttl", "owner", 30*time.Millisecond)
	testx.NoError(t, err)
	testx.True(t, ok)

	testx.NoError(t, locker.Expire(ctx, "lock-ttl", time.Minute))
	time.Sleep(50 * time.Millisecond)

	// TTL 已被 Expire 续到 1 分钟，50ms 后锁仍有效。
	ok2, err := locker.SetNX(ctx, "lock-ttl", "other", time.Minute)
	testx.NoError(t, err)
	testx.False(t, ok2, "续期后锁不应过期")
}
