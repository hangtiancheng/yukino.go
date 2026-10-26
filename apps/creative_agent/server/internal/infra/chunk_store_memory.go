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

// chunkStoreSeq 为每个 MemoryChunkStore 实例生成唯一的 yukino_cache group 名，
// 避免进程内多实例（测试并行）在 group 注册表中重名。
var chunkStoreSeq atomic.Int64

// MemoryChunkStore 是 ChunkStore 的进程内实现，底层用 yukino_cache Group 存储
// 每个 msgID 的 chunk 列表（JSON 编码，带 TTL），通知走进程内 channel 广播。
//
// 适用于单二进制部署（无 Redis）与单元测试（替代 miniredis）。
// 键布局与 RedisChunkStore 对齐：
//   - {msgID}          → chunk 列表 JSON
//   - end:{msgID}      → end-of-stream 标记（"1"）
type MemoryChunkStore struct {
	group *yukino_cache.Group
	ttl   time.Duration

	keysMu sync.Mutex
	keyMu  map[string]*sync.Mutex

	notifyMu sync.Mutex
	notify   map[string][]chan struct{}
}

// NewMemoryChunkStore 创建基于 yukino_cache 的进程内 ChunkStore。
func NewMemoryChunkStore() *MemoryChunkStore {
	name := fmt.Sprintf("cg:chunk:memory:%d", chunkStoreSeq.Add(1))
	cs := &MemoryChunkStore{
		ttl:    1 * time.Hour,
		keyMu:  make(map[string]*sync.Mutex),
		notify: make(map[string][]chan struct{}),
	}
	// getter 恒返回空：chunk 数据只经 Write 写入，无回源加载语义。
	cs.group = yukino_cache.NewGroup(name, 16<<20,
		yukino_cache.GetterFunc(func(_ context.Context, _ string) ([]byte, error) {
			return nil, nil
		}),
		yukino_cache.WithExpiration(cs.ttl),
	)
	return cs
}

// Close 关闭底层缓存 group（测试清理用；进程级实例可不关闭）。
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

// Write 追加一个 chunk 并广播变更通知；endTurn 时写入 end 标记。
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

// After 先订阅通知再读取存量 chunk（与 RedisChunkStore 相同的 race-free 顺序），
// 返回 cursor 之后的 chunk、新游标与变更通知 channel（ctx 取消时关闭）。
func (cs *MemoryChunkStore) After(ctx context.Context, msgID string, cursor int64) ([]Chunk, int64, <-chan struct{}, error) {
	// Step 1: 先订阅，避免读到订阅之间的写入丢失。
	notify := cs.subscribe(msgID)
	go func() {
		<-ctx.Done()
		cs.unsubscribe(msgID, notify)
		close(notify)
	}()

	// Step 2: 读取存量。
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

// HasEndTurn 检查 end-of-stream 标记是否存在。
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
