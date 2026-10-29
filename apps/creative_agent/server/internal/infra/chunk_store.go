// Package infra provides Redis-based infrastructure abstractions for the
// async conversation pipeline: ChunkStore (keyed append-only payload store
// with change notification) and StreamQueue (reliable message queue with
// consumer groups).
package infra

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Chunk represents a single frame in the async data pipeline.
// Worker writes chunks into Redis via ChunkStore.Write; RunAttach replays them
// into the HTTP flow context via ChunkStore.After.
type Chunk struct {
	// Payload is the SSE frame JSON string (e.g. {"type":"text-delta","content":"Hello"}).
	Payload string `json:"payload"`
	// EndTurn marks the terminal "end of reply" chunk. When true, Payload is empty,
	// and RunAttach exits after replaying this chunk.
	EndTurn bool `json:"endTurn"`
	// MsgID is the business message ID this chunk belongs to.
	MsgID string `json:"msgId"`
	// Seq is the position in the list (1-based), assigned by After on read.
	Seq int64 `json:"seq"`
}

// ChunkStore is a keyed append-only payload store with per-key end-of-stream
// marker and change notification via Redis Pub/Sub.
//
// Lifecycle: Worker calls Write to produce chunks → RunAttach calls After to
// consume them with subscribe-then-read semantics.
type ChunkStore interface {
	// Write appends a chunk to the list and broadcasts a notification.
	// If endTurn is true, also sets an end-of-stream marker for HasEndTurn checks.
	Write(ctx context.Context, msgID, payload string, endTurn bool) error

	// After subscribes to change notifications FIRST, then reads existing chunks
	// from cursor onwards (race-free: subscribe before read).
	// Returns current chunks, new cursor, and a channel that fires on new writes.
	After(ctx context.Context, msgID string, cursor int64) ([]Chunk, int64, <-chan struct{}, error)

	// HasEndTurn checks whether the end-of-stream marker exists (for zombie recovery).
	HasEndTurn(ctx context.Context, msgID string) (bool, error)
}

// RedisChunkStore implements ChunkStore on top of Redis Lists + Pub/Sub.
//
// Redis keys used:
//   - cg:chunk:{msgID}   → Redis List (RPUSH for write, LRANGE for read)
//   - cg:chunk:end:{msgID} → Redis String (SET on endTurn, EXISTS for HasEndTurn)
//   - cg:chunk:notify:{msgID} → Redis Pub/Sub channel (PUBLISH on write)
//
// All keys expire after 1 hour (TTL) to prevent leaks from abandoned messages.
type RedisChunkStore struct {
	rdb *redis.Client // Redis client
	ttl time.Duration // expiry for all keys, default 1h
}

// NewRedisChunkStore creates a Redis-backed ChunkStore.
// Keys are namespaced as cg:chunk:{msgID} (list) and cg:chunk:end:{msgID} (marker).
// Pub/Sub channel: cg:chunk:notify:{msgID}.
func NewRedisChunkStore(rdb *redis.Client) *RedisChunkStore {
	return &RedisChunkStore{rdb: rdb, ttl: 1 * time.Hour}
}

func chunkListKey(msgID string) string  { return fmt.Sprintf("cg:chunk:%s", msgID) }
func chunkEndKey(msgID string) string   { return fmt.Sprintf("cg:chunk:end:%s", msgID) }
func chunkNotifyCh(msgID string) string { return fmt.Sprintf("cg:chunk:notify:%s", msgID) }

// Write appends a Chunk to the list and publishes a notification.
// If endTurn is true, also sets the end marker key.
func (cs *RedisChunkStore) Write(ctx context.Context, msgID, payload string, endTurn bool) error {
	listKey := chunkListKey(msgID)
	notifyCh := chunkNotifyCh(msgID)

	// Compute the real sequence number (1-based): LLEN gives the current length; +1 yields this entry's position.
	// Previously seq was always 0, causing the frontend's "frame count" resume cursor to drift from the LRANGE cursor.
	curLen, err := cs.rdb.LLen(ctx, listKey).Result()
	if err != nil {
		return fmt.Errorf("chunk_store: llen: %w", err)
	}

	chunk := Chunk{
		Payload: payload,
		EndTurn: endTurn,
		MsgID:   msgID,
		Seq:     curLen + 1,
	}

	b, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("chunk_store: marshal: %w", err)
	}

	// RPUSH + EXPIRE
	pipe := cs.rdb.Pipeline()
	pipe.RPush(ctx, listKey, string(b))
	pipe.Expire(ctx, listKey, cs.ttl)
	pipe.Publish(ctx, notifyCh, "1")
	if endTurn {
		pipe.Set(ctx, chunkEndKey(msgID), "1", cs.ttl)
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("chunk_store: write pipeline: %w", err)
	}
	return nil
}

// After returns chunks from cursor onwards, then sets up a subscription for new
// chunks. It subscribes FIRST then reads, ensuring no messages are lost between
// the read and the subscribe (race-free pattern).
//
// Returns current chunks, new cursor, and a notification channel that fires
// each time a new chunk is written (via Redis Pub/Sub). The channel closes when
// the context is cancelled.
func (cs *RedisChunkStore) After(ctx context.Context, msgID string, cursor int64) ([]Chunk, int64, <-chan struct{}, error) {
	listKey := chunkListKey(msgID)
	notifyCh := chunkNotifyCh(msgID)

	// Step 1: Subscribe FIRST (before reading, to avoid race)
	pubsub := cs.rdb.Subscribe(ctx, notifyCh)
	// Wait for subscription confirmation
	if _, err := pubsub.Receive(ctx); err != nil {
		pubsub.Close()
		return nil, cursor, nil, fmt.Errorf("chunk_store: subscribe: %w", err)
	}

	// Build a Go channel from Redis Pub/Sub messages
	notify := make(chan struct{}, 1)
	go func() {
		defer pubsub.Close()
		defer close(notify)
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				select {
				case notify <- struct{}{}:
				default:
				}
			}
		}
	}()

	// Step 2: Read existing chunks (race-free because we subscribed first)
	vals, err := cs.rdb.LRange(ctx, listKey, cursor, -1).Result()
	if err != nil && err != redis.Nil {
		return nil, cursor, nil, fmt.Errorf("chunk_store: lrange: %w", err)
	}

	var chunks []Chunk
	for i, raw := range vals {
		var c Chunk
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			continue
		}
		c.Seq = cursor + int64(i) + 1
		chunks = append(chunks, c)
	}

	newCursor := cursor + int64(len(vals))
	return chunks, newCursor, notify, nil
}

// HasEndTurn checks whether the end-of-turn marker exists for msgID.
func (cs *RedisChunkStore) HasEndTurn(ctx context.Context, msgID string) (bool, error) {
	n, err := cs.rdb.Exists(ctx, chunkEndKey(msgID)).Result()
	if err != nil {
		return false, fmt.Errorf("chunk_store: exists: %w", err)
	}
	return n > 0, nil
}
