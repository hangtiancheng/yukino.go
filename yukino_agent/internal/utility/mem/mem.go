// Package mem provides an in-memory conversation history manager.
// It stores per-session message lists with a sliding window to keep
// memory usage bounded during multi-turn conversations.
package mem

import (
	"container/list"
	"sync"

	"github.com/cloudwego/eino/schema"
)

// MaxSessions caps the number of concurrent conversation sessions kept in
// memory. When the cap is exceeded the least-recently-used session is evicted,
// mirroring the Next.js MAX_SESSIONS=100 LRU policy in lib/memory.ts.
const MaxSessions = 100

var (
	mu      sync.Mutex
	memMap  = make(map[string]*ConversationMemory)
	lruList = list.New() // front = least recently used, back = most recently used
)

// Get retrieves or creates a ConversationMemory for the given session ID.
// On access the entry is moved to the back (most recently used) of the LRU
// list; when the capacity is exceeded the front (oldest) entry is evicted.
// The returned memory is safe for concurrent use.
func Get(id string) *ConversationMemory {
	mu.Lock()
	defer mu.Unlock()

	if m, ok := memMap[id]; ok {
		lruList.MoveToBack(m.element)
		return m
	}

	// Evict the least recently used session when at capacity.
	if len(memMap) >= MaxSessions {
		if front := lruList.Front(); front != nil {
			oldest := front.Value.(*ConversationMemory)
			lruList.Remove(front)
			delete(memMap, oldest.ID)
		}
	}

	m := &ConversationMemory{
		ID:            id,
		Messages:      []*schema.Message{},
		MaxWindowSize: 6,
	}
	m.element = lruList.PushBack(m)
	memMap[id] = m
	return m
}

// ConversationMemory holds a bounded sliding window of conversation messages
// for a single chat session.
type ConversationMemory struct {
	ID            string            `json:"id"`
	Messages      []*schema.Message `json:"messages"`
	MaxWindowSize int
	element       *list.Element // back-reference for O(1) LRU reordering
	mu            sync.Mutex
}

// Append adds a message to the conversation history.
// When the history exceeds MaxWindowSize, the oldest messages are
// removed in pairs to maintain the user/assistant message pairing.
func (m *ConversationMemory) Append(msg *schema.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Messages = append(m.Messages, msg)
	if len(m.Messages) > m.MaxWindowSize {
		// Ensure even number of dropped messages to preserve conversation pairing.
		excess := len(m.Messages) - m.MaxWindowSize
		if excess%2 != 0 {
			excess++
		}
		m.Messages = m.Messages[excess:]
	}
}

// All returns a snapshot copy of the current conversation messages.
// A copy is returned (rather than the internal slice) so callers cannot
// mutate the memory's state through the returned reference.
func (m *ConversationMemory) All() []*schema.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*schema.Message(nil), m.Messages...)
}
