// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
