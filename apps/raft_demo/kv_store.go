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

package main

import (
	"encoding/json"
	"sync"
)

type kvStore struct {
	proposeC chan<- string
	sync.RWMutex
	core map[string]string
}

type kv struct {
	Key string `json:"key"`
	Val string `json:"val"`
}

func newKVStore(proposeC chan<- string, commitC <-chan *string) *kvStore {
	k := kvStore{
		proposeC: proposeC,
		core:     make(map[string]string),
	}

	go k.readCommit(commitC)
	return &k
}

func (k *kvStore) readCommit(commitC <-chan *string) {
	for data := range commitC {
		// Skip nil markers and malformed payloads instead of applying a
		// zero-valued entry to the state machine
		if data == nil {
			continue
		}

		// Apply committed data to the state machine
		var kv kv
		if err := json.Unmarshal([]byte(*data), &kv); err != nil {
			continue
		}

		k.Lock()
		k.core[kv.Key] = kv.Val
		k.Unlock()
	}
}

func (k *kvStore) Propose(key, val string) {
	body, _ := json.Marshal(kv{Key: key, Val: val})
	k.proposeC <- string(body)
}
