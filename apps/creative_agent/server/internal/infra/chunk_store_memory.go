package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	yukino_cache "github.com/hangtiancheng/yukino.go/libs/yukino_cache"
)

// chunkStoreSeq generates a unique yukino_cache group name per MemoryChunkStore instance,
// avoiding name collisions in the group registry when multiple instances exist in one process (parallel tests).
var chunkStoreSeq atomic.Int64

// MemoryChunkStore is the in-process implementation of ChunkStore, backed by a yukino_cache Group storing
// the chunk list per msgID (JSON-encoded, with TTL); notifications are broadcast over in-process channels.
//
// Suitable for single-binary deployments (no Redis) and unit tests (replacing miniredis).
// The key layout aligns with RedisChunkStore:
//   - {msgID}          → chunk-list JSON
//   - end:{msgID}      → end-of-stream marker ("1")
type MemoryChunkStore struct {
	group *yukino_cache.Group
	ttl   time.Duration

	keysMu sync.Mutex
	keyMu  map[string]*sync.Mutex

	notifyMu sync.Mutex
	notify   map[string][]chan struct{}
}

// NewMemoryChunkStore creates the yukino_cache-backed in-process ChunkStore.
func NewMemoryChunkStore() *MemoryChunkStore {
	name := fmt.Sprintf("cg:chunk:memory:%d", chunkStoreSeq.Add(1))
	cs := &MemoryChunkStore{
		ttl:    1 * time.Hour,
		keyMu:  make(map[string]*sync.Mutex),
		notify: make(map[string][]chan struct{}),
	}
	// The getter always returns empty: chunk data is only written via Write; there is no load-from-source semantics.
	cs.group = yukino_cache.NewGroup(name, 16<<20,
		yukino_cache.GetterFunc(func(_ context.Context, _ string) ([]byte, error) {
			return nil, nil
		}),
		yukino_cache.WithExpiration(cs.ttl),
	)
	return cs
}

// Close shuts down the underlying cache group (for test cleanup; process-level instances may skip it).
func (cs *MemoryChunkStore) Close() error { return cs.group.Close() }

func (cs *MemoryChunkStore) lockFor(msgID string) *sync.Mutex {
	cs.keysMu.Lock()
	defer cs.keysMu.Unlock()
	mu, ok := cs.keyMu[msgID]
	if !ok {
		mu = &sync.Mutex{}
		cs.keyMu[msgID] = mu
	}
	return mu
}

// Write appends a chunk and broadcasts a change notification; on endTurn it writes the end marker.
func (cs *MemoryChunkStore) Write(ctx context.Context, msgID, payload string, endTurn bool) error {
	mu := cs.lockFor(msgID)
	mu.Lock()
	defer mu.Unlock()

	chunks := cs.readAll(ctx, msgID)
	chunks = append(chunks, Chunk{
		Payload: payload,
		EndTurn: endTurn,
		MsgID:   msgID,
		Seq:     int64(len(chunks) + 1),
	})
	if err := cs.writeAll(ctx, msgID, chunks); err != nil {
		return err
	}
	if endTurn {
		if err := cs.group.Set(ctx, "end:"+msgID, []byte("1")); err != nil {
			return fmt.Errorf("chunk_store: set end marker: %w", err)
		}
	}
	cs.broadcast(msgID)
	return nil
}

func (cs *MemoryChunkStore) readAll(ctx context.Context, msgID string) []Chunk {
	view, err := cs.group.Get(ctx, msgID)
	if err != nil {
		return nil
	}
	raw := view.ByteSlice()
	if len(raw) == 0 {
		return nil
	}
	var chunks []Chunk
	if err := json.Unmarshal(raw, &chunks); err != nil {
		return nil
	}
	return chunks
}

func (cs *MemoryChunkStore) writeAll(ctx context.Context, msgID string, chunks []Chunk) error {
	b, err := json.Marshal(chunks)
	if err != nil {
		return fmt.Errorf("chunk_store: marshal: %w", err)
	}
	if err := cs.group.Set(ctx, msgID, b); err != nil {
		return fmt.Errorf("chunk_store: write: %w", err)
	}
	return nil
}

// After subscribes to notifications before reading existing chunks (the same race-free order as RedisChunkStore),
// returning the chunks after the cursor, the new cursor, and the change-notification channel (closed when ctx is canceled).
func (cs *MemoryChunkStore) After(ctx context.Context, msgID string, cursor int64) ([]Chunk, int64, <-chan struct{}, error) {
	// Step 1: subscribe first, so writes happening between subscription and read are not lost.
	notify := cs.subscribe(msgID)
	go func() {
		<-ctx.Done()
		cs.unsubscribe(msgID, notify)
		close(notify)
	}()

	// Step 2: read the existing data.
	mu := cs.lockFor(msgID)
	mu.Lock()
	defer mu.Unlock()
	all := cs.readAll(ctx, msgID)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > int64(len(all)) {
		cursor = int64(len(all))
	}
	chunks := make([]Chunk, 0, len(all)-int(cursor))
	for i, c := range all[cursor:] {
		c.Seq = cursor + int64(i) + 1
		chunks = append(chunks, c)
	}
	return chunks, cursor + int64(len(chunks)), notify, nil
}

// HasEndTurn checks whether the end-of-stream marker exists.
func (cs *MemoryChunkStore) HasEndTurn(ctx context.Context, msgID string) (bool, error) {
	view, err := cs.group.Get(ctx, "end:"+msgID)
	if err != nil {
		return false, err
	}
	return len(view.ByteSlice()) > 0, nil
}

func (cs *MemoryChunkStore) subscribe(msgID string) chan struct{} {
	ch := make(chan struct{}, 1)
	cs.notifyMu.Lock()
	cs.notify[msgID] = append(cs.notify[msgID], ch)
	cs.notifyMu.Unlock()
	return ch
}

func (cs *MemoryChunkStore) unsubscribe(msgID string, target chan struct{}) {
	cs.notifyMu.Lock()
	defer cs.notifyMu.Unlock()
	list := cs.notify[msgID]
	for i, ch := range list {
		if ch == target {
			cs.notify[msgID] = append(list[:i], list[i+1:]...)
			break
		}
	}
}

func (cs *MemoryChunkStore) broadcast(msgID string) {
	cs.notifyMu.Lock()
	defer cs.notifyMu.Unlock()
	for _, ch := range cs.notify[msgID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
