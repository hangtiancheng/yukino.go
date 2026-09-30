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
