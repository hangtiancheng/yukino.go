package mem

import (
	"container/list"
	"sync"

	"github.com/cloudwego/eino/schema"
)

const MaxSessions = 100

var (
	mu      sync.Mutex
	memMap  = make(map[string]*ConversationMemory)
	lruList = list.New()
)

func Get(id string) *ConversationMemory {
	mu.Lock()
	defer mu.Unlock()

	if m, ok := memMap[id]; ok {
		lruList.MoveToBack(m.element)
		return m
	}

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

type ConversationMemory struct {
	ID            string            `json:"id"`
	Messages      []*schema.Message `json:"messages"`
	MaxWindowSize int
	element       *list.Element
	mu            sync.Mutex
}

func (m *ConversationMemory) Append(msg *schema.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Messages = append(m.Messages, msg)
	if len(m.Messages) > m.MaxWindowSize {
		excess := len(m.Messages) - m.MaxWindowSize
		if excess%2 != 0 {
			excess++
		}
		m.Messages = m.Messages[excess:]
	}
}

func (m *ConversationMemory) All() []*schema.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*schema.Message(nil), m.Messages...)
}
