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
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/raft_demo/raft"
)

type raftProxy struct {
	// Channel for user-submitted write proposals
	proposeC <-chan string
	// Channel for user-submitted configuration change proposals
	confChangeC <-chan raft.ConfChange
	// Channel for committed log entries
	commitC chan<- *string
	// Client node ID
	id uint64
	// Peer node list
	peers []string

	// Raft node instance
	node raft.Node

	// Log persistence storage
	storage raft.Storage
}

func newRaftProxy(id uint64, peers []string, proposeC <-chan string, confChangeC <-chan raft.ConfChange) <-chan *string {
	commitC := make(chan *string)
	r := raftProxy{
		proposeC:    proposeC,
		confChangeC: confChangeC,
		commitC:     commitC,
		id:          id,
		peers:       peers,
		storage:     raft.NewMemoryStorage(),
	}

	go r.run()
	return commitC
}

func (r *raftProxy) run() {
	peers := make([]raft.Peer, 0, len(r.peers))
	for i := range r.peers {
		peers = append(peers, raft.Peer{ID: uint64(i + 1)})
	}

	c := raft.Config{
		ID:            uint64(r.id),
		ElectionTick:  10,
		HeartbeatTick: 1,
		Storage:       r.storage,
	}

	r.node = raft.StartNode(&c, peers)

	// Start transport module

	// Start listener module
	go r.listen()
}

func (r *raftProxy) listen() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Listen for client proposal channels
	go r.listenRequest()
	// Main loop: listen for ready state

	for {
		select {
		case <-ticker.C:
			r.node.Tick()

		case <-r.node.Ready():
			// Persist hard state and configuration

			// Persist log entries

			// Send messages to peers

			// Apply committed log entries to state machine

			// advance
			r.node.Advance()
		}
	}
}

func (r *raftProxy) listenRequest() {
	for {
		select {
		case prop, ok := <-r.proposeC:
			if !ok {
				return
			}
			r.node.Propose(context.Background(), []byte(prop))

		case cc, ok := <-r.confChangeC:
			if !ok {
				return
			}
			r.node.ProposeConfChange(context.Background(), cc)
		}
	}

}
