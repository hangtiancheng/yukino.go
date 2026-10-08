package codec

import (
	"fmt"
	"sync"
)

type Codec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
}

type Type byte

type Factory func() Codec

var (
	mu        sync.RWMutex
	factories = make(map[Type]Factory)
)

func Register(t Type, f Factory) {
	mu.Lock()
	defer mu.Unlock()

	if f == nil {
		panic("codec: factory is nil")
	}

	if _, exists := factories[t]; exists {
		panic(fmt.Sprintf("codec: type %d already registered", t))
	}

	factories[t] = f
}

func New(t Type) (Codec, error) {
	mu.RLock()
	defer mu.RUnlock()

	f, ok := factories[t]
	if !ok {
		return nil, fmt.Errorf("codec: type %d not registered", t)
	}

	return f(), nil
}
