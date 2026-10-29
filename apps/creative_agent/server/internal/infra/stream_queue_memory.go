package infra

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// memStreamEntry is one message in the in-memory queue.
type memStreamEntry struct {
	ID          string
	MsgID       string
	Body        []byte
	deliveredAt time.Time
}

// MemoryStreamQueue is the in-process implementation of StreamQueue (a degradation for single-binary deployments without Redis;
// replaces miniredis in tests). Semantics align with RedisStreamQueue:
//   - Push appends a message; Receive blocks pulling undelivered messages (returns nil, nil on timeout)
//   - Ack removes from pending; ClaimPending claims pending messages idle beyond a timeout
//
// All consumers within one process share a single delivery cursor (equivalent to one consumer group).
type MemoryStreamQueue struct {
	mu        sync.Mutex
	entries   []*memStreamEntry
	cursor    int
	pending   map[string]*memStreamEntry
	seq       int64
	signal    chan struct{}
	blockTime time.Duration
}

// NewMemoryStreamQueue creates the in-process StreamQueue. streamKey/group/consumer exist only to keep
// the same constructor signature as RedisStreamQueue (the in-process implementation does not distinguish keys).
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

// Push appends a message and wakes waiting Receives.
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

// Receive blocks pulling the next undelivered message; returns nil, nil when no message arrives within blockTime;
// returns ctx.Err() when ctx is canceled.
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

// Ack acknowledges a message, removing it from the pending list.
func (q *MemoryStreamQueue) Ack(_ context.Context, streamMsgID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.pending, streamMsgID)
	return nil
}

// Requeue re-enqueues a message (as a new entry; the old entry stays pending until Acked or claimed).
func (q *MemoryStreamQueue) Requeue(ctx context.Context, msgID string, body []byte) error {
	return q.Push(ctx, msgID, body)
}

// ClaimPending claims pending messages idle longer than minIdle (for zombie recovery).
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
