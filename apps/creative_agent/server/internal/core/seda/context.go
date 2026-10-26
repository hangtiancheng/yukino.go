// Package seda 提供 SEDA（阶段式事件驱动架构）执行引擎。
//
// Context 是一次 flow 执行的共享数据容器，支持两种数据形态：
//   - 单值（Value）：Set/Get/Wait，一个 key 存一个值，可覆盖
//   - 流式（Slice）：Push/GetSliceWithInit，一个 key 存一条可追加序列，
//     带变更通知，用于实时流式场景（如 SSE 帧）。
package seda

import "sync"

// ---------- Slice：可追加流式通道 ----------

// Slice 是一个线程安全的可追加列表，带变更通知机制。
// 它是流式数据（如 SSE 帧）的基础构件。
// 每次 Push 追加元素并广播；消费者通过 WatchPush+DiffFrom
// 只接收上次读取之后新增的元素。
type Slice struct {
	mu     sync.Mutex
	items  []any
	notify chan struct{} // 每次 push 换新 channel，close 旧的即广播唤醒
}

func newSlice() *Slice {
	return &Slice{notify: make(chan struct{})}
}

// Push 追加元素 v，并关闭旧的通知 channel、创建新的供下一轮等待，
// 以此唤醒所有当前 WatchPush 的等待者。
func (s *Slice) Push(v any) {
	s.mu.Lock()
	s.items = append(s.items, v)
	old := s.notify
	s.notify = make(chan struct{})
	s.mu.Unlock()
	close(old)
}

// WatchPush 返回当前的通知 channel。调用方每次被唤醒后必须重新调
// WatchPush，才能拿到下一轮等待用的 channel。
func (s *Slice) WatchPush() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notify
}

// DiffFrom 返回自 cursor 位置之后追加的所有元素，以及新的 cursor
// （= 读取后总长度）。返回的切片是浅拷贝，可以安全修改。
func (s *Slice) DiffFrom(cursor int) ([]any, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cursor >= len(s.items) {
		return nil, cursor
	}
	diff := make([]any, len(s.items)-cursor)
	copy(diff, s.items[cursor:])
	return diff, len(s.items)
}

// ---------- Context ----------

// Context 是一次 flow 执行的共享数据上下文。
type Context struct {
	mu      sync.Mutex
	data    map[string]any
	waiters map[string]chan struct{} // 每个 key 一个通知 channel（单值订阅用）
	slices  map[string]*Slice        // 每个 key 一条流式通道（流式订阅用）
}

// NewContext 创建一个空的 Context。
func NewContext() *Context {
	return &Context{
		data:    make(map[string]any),
		waiters: make(map[string]chan struct{}),
		slices:  make(map[string]*Slice),
	}
}

// chanOf 惰性返回（或创建）key k 的通知 channel。必须在 c.mu 锁内调用。
func (c *Context) chanOf(k string) chan struct{} {
	ch, ok := c.waiters[k]
	if !ok {
		ch = make(chan struct{})
		c.waiters[k] = ch
	}
	return ch
}

// Set 为 key k 存入值 v，并广播唤醒所有阻塞在 Wait(k) 上的 goroutine。
func (c *Context) Set(k string, v any) {
	c.mu.Lock()
	c.data[k] = v
	ch := c.chanOf(k)
	c.mu.Unlock()
	// 用 close 做一对多广播。select 防重复 close panic。
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// Get 返回 key k 对应的值，不存在返回 nil。
func (c *Context) Get(k string) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data[k]
}

// Wait 阻塞直到 key k 被 Set，然后返回其值。
// 若 k 已被 Set，则立即返回。
func (c *Context) Wait(k string) any {
	c.mu.Lock()
	if v, ok := c.data[k]; ok {
		c.mu.Unlock()
		return v
	}
	ch := c.chanOf(k)
	c.mu.Unlock()

	<-ch
	return c.Get(k)
}

// GetSliceWithInit 返回 key k 对应的 Slice，不存在时自动创建。
// 一定要在 Push 之前调用，避免丢失第一帧。
func (c *Context) GetSliceWithInit(k string) *Slice {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.slices[k]
	if !ok {
		s = newSlice()
		c.slices[k] = s
	}
	return s
}

// Push 将 v 追加到 key k 对应的 Slice 尾部，Slice 不存在时自动创建。
func (c *Context) Push(k string, v any) {
	c.GetSliceWithInit(k).Push(v)
}
