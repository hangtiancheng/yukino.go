package infra

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// memStreamEntry 是内存队列中的一条消息。
type memStreamEntry struct {
	ID          string
	MsgID       string
	Body        []byte
	deliveredAt time.Time
}

// MemoryStreamQueue 是 StreamQueue 的进程内实现（单二进制部署无 Redis 时降级用，
// 测试中替代 miniredis）。语义对齐 RedisStreamQueue：
//   - Push 追加消息；Receive 阻塞拉取未投递消息（超时返回 nil, nil）
//   - Ack 从 pending 移除；ClaimPending 认领空闲超时的 pending 消息
//
// 单进程内所有 consumer 共享一条投递游标（等价于同一 consumer group）。
type MemoryStreamQueue struct {
	mu        sync.Mutex
	entries   []*memStreamEntry
	cursor    int
	pending   map[string]*memStreamEntry
	seq       int64
	signal    chan struct{}
	blockTime time.Duration
}

// NewMemoryStreamQueue 创建进程内 StreamQueue。streamKey/group/consumer 仅用于
// 与 RedisStreamQueue 保持相同的构造签名（进程内实现不区分 key）。
func NewMemoryStreamQueue(streamKey, group, consumer string) *MemoryStreamQueue {
	_ = streamKey
	_ = group
	_ = consumer
	return &MemoryStreamQueue{
		pending:   make(map[string]*memStreamEntry),
		signal:    make(chan struct{}),
		blockTime: 5 * time.Second,
	}
}

// Push 追加一条消息并唤醒等待中的 Receive。
func (q *MemoryStreamQueue) Push(_ context.Context, msgID string, body []byte) error {
	q.mu.Lock()
	q.seq++
	entry := &memStreamEntry{
		ID:    fmt.Sprintf("%d-%d", time.Now().UnixMilli(), q.seq),
		MsgID: msgID,
		Body:  append([]byte(nil), body...),
	}
	q.entries = append(q.entries, entry)
	signal := q.signal
	q.signal = make(chan struct{})
	q.mu.Unlock()
	close(signal)
	return nil
}

// Receive 阻塞拉取下一条未投递消息；blockTime 内无消息返回 nil, nil；
// ctx 取消返回 ctx.Err()。
func (q *MemoryStreamQueue) Receive(ctx context.Context) (*StreamMessage, error) {
	deadline := time.NewTimer(q.blockTime)
	defer deadline.Stop()
	for {
		q.mu.Lock()
		if q.cursor < len(q.entries) {
			entry := q.entries[q.cursor]
			q.cursor++
			entry.deliveredAt = time.Now()
			q.pending[entry.ID] = entry
			q.mu.Unlock()
			return &StreamMessage{ID: entry.ID, MsgID: entry.MsgID, Body: entry.Body}, nil
		}
		signal := q.signal
		q.mu.Unlock()

		select {
		case <-signal:
		case <-deadline.C:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Ack 确认消息，从 pending 列表移除。
func (q *MemoryStreamQueue) Ack(_ context.Context, streamMsgID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, streamMsgID)
	return nil
}

// Requeue 将消息重新入队（新条目；旧条目留在 pending 直到被 Ack 或认领）。
func (q *MemoryStreamQueue) Requeue(ctx context.Context, msgID string, body []byte) error {
	return q.Push(ctx, msgID, body)
}

// ClaimPending 认领空闲时长超过 minIdle 的 pending 消息（僵尸恢复用）。
func (q *MemoryStreamQueue) ClaimPending(_ context.Context, minIdle time.Duration) ([]*StreamMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	var claimed []*memStreamEntry
	for _, entry := range q.pending {
		if now.Sub(entry.deliveredAt) >= minIdle {
			entry.deliveredAt = now
			claimed = append(claimed, entry)
		}
	}
	sort.Slice(claimed, func(i, j int) bool { return claimed[i].ID < claimed[j].ID })

	msgs := make([]*StreamMessage, 0, len(claimed))
	for _, entry := range claimed {
		msgs = append(msgs, &StreamMessage{ID: entry.ID, MsgID: entry.MsgID, Body: entry.Body})
	}
	return msgs, nil
}
