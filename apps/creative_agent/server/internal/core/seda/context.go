// Package seda provides the SEDA (staged event-driven architecture) execution engine.
//
// Context is the shared data container of one flow execution, supporting two data shapes:
//   - Single value (Value): Set/Get/Wait, one key stores one value, overwritable
//   - Streaming (Slice): Push/GetSliceWithInit, one key stores an appendable sequence
//     with change notifications, for realtime streaming scenarios (e.g. SSE frames).
package seda

import "sync"

// ---------- Slice: appendable streaming channel ----------

// Slice is a thread-safe appendable list with a change-notification mechanism.
// It is the building block of streaming data (e.g. SSE frames).
// Each Push appends an element and broadcasts; consumers use WatchPush+DiffFrom
// to receive only elements added since their last read.
type Slice struct {
	mu     sync.Mutex
	items  []any
	notify chan struct{} // each push swaps in a new channel; closing the old one broadcasts the wakeup
}

func newSlice() *Slice {
	return &Slice{notify: make(chan struct{})}
}

// Push appends element v, closes the old notification channel, and creates a new one for the next wait round,
// thereby waking all waiters currently on WatchPush.
func (s *Slice) Push(v any) {
	s.mu.Lock()
	s.items = append(s.items, v)
	old := s.notify
	s.notify = make(chan struct{})
	s.mu.Unlock()
	close(old)
}

// WatchPush returns the current notification channel. Callers must call WatchPush again
// after each wakeup to obtain the channel for the next wait round.
func (s *Slice) WatchPush() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notify
}

// DiffFrom returns all elements appended after the cursor position, plus the new cursor
// (= total length after the read). The returned slice is a shallow copy and safe to modify.
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

// Context is the shared data context of one flow execution.
type Context struct {
	mu      sync.Mutex
	data    map[string]any
	waiters map[string]chan struct{} // one notification channel per key (for single-value subscription)
	slices  map[string]*Slice        // one streaming channel per key (for streaming subscription)
}

// NewContext creates an empty Context.
func NewContext() *Context {
	return &Context{
		data:    make(map[string]any),
		waiters: make(map[string]chan struct{}),
		slices:  make(map[string]*Slice),
	}
}

// chanOf lazily returns (or creates) the notification channel of key k. Must be called under the c.mu lock.
func (c *Context) chanOf(k string) chan struct{} {
	ch, ok := c.waiters[k]
	if !ok {
		ch = make(chan struct{})
		c.waiters[k] = ch
	}
	return ch
}

// Set stores value v under key k and broadcasts, waking all goroutines blocked on Wait(k).
func (c *Context) Set(k string, v any) {
	c.mu.Lock()
	c.data[k] = v
	ch := c.chanOf(k)
	c.mu.Unlock()
	// Use close for one-to-many broadcast. The select guards against a double-close panic.
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// Get returns the value of key k, or nil when absent.
func (c *Context) Get(k string) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.data[k]
}

// Wait blocks until key k is Set, then returns its value.
// It returns immediately if k has already been Set.
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

// GetSliceWithInit returns the Slice of key k, creating it automatically when absent.
// It must be called before Push to avoid losing the first frame.
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

// Push appends v to the tail of the Slice of key k, creating the Slice automatically when absent.
func (c *Context) Push(k string, v any) {
	c.GetSliceWithInit(k).Push(v)
}
