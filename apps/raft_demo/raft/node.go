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

package raft

import (
	"context"
	"encoding/json"
)

type Node struct {
	// Channel for local proposals
	proc chan Message
	// Channel for messages from peers
	recvChan chan Message
	// Channel delivering committed configuration changes to be applied
	confChan chan ConfChange
	// Channel returning the resulting membership of an applied conf change
	confStateChan chan ConfState
	// Channel delivering ready state to the application
	readyChan chan Ready
	// Channel signaling the application has applied ready state
	advanceChan chan struct{}
	// Tick channel
	tickChan chan struct{}
}

func StartNode(conf *Config, peers []Peer) Node {
	r := newRaft(conf)
	// Register all peers in the configuration
	for _, peer := range peers {
		r.addNode(peer.ID)
	}
	r.raftLog.commitIndex = r.raftLog.lastIndex()

	n := newNode()
	go n.run(r)
	return n
}

func newNode() Node {
	return Node{
		proc:          make(chan Message),
		recvChan:      make(chan Message),
		confChan:      make(chan ConfChange),
		confStateChan: make(chan ConfState),
		readyChan:     make(chan Ready),
		advanceChan:   make(chan struct{}),
		tickChan:      make(chan struct{}),
	}
}

func (n *Node) run(r *raft) {
	var (
		readyChan   chan Ready
		advanceChan chan struct{}
		// Ready state
		rd       Ready
		prevSoft = r.softState()
		prevHard = emptyHardState

		prevLastUnstableI, prevLastUnstableT uint64
		hasPrevLastUnstableI                 bool
	)

	for {
		// Check for state updates and deliver via readyChan if present
		if advanceChan != nil {
			readyChan = nil
		} else if rd = newReady(r, prevSoft, prevHard); rd.containsUpdates() {
			readyChan = n.readyChan
		} else {
			readyChan = nil
		}

		select {
		// Received a local proposal
		case m := <-n.proc:
			// Process local proposal message
			m.From = r.id
			r.Step(m)
		case m := <-n.recvChan:
			// Process a message from a peer or a forwarded client request
			r.Step(m)
		case cc := <-n.confChan:
			// Apply a committed configuration change
			n.confStateChan <- r.applyConfChange(cc)
		case <-n.tickChan:
			// Timer tick
			r.tick()
		case readyChan <- rd:
			// Sync updated state for next iteration's change detection
			if rd.SoftState != nil {
				prevSoft = rd.SoftState
			}

			if !IsEmptyHardState(rd.HardState) {
				prevHard = rd.HardState
			}

			if len(rd.Entries) > 0 {
				prevLastUnstableI = rd.Entries[len(rd.Entries)-1].Index
				prevLastUnstableT = rd.Entries[len(rd.Entries)-1].Term
				hasPrevLastUnstableI = true
			}

			r.msgs = nil
			r.readStates = nil
			advanceChan = n.advanceChan
		case <-advanceChan:
			// The application has consumed the ready: persist the delivered
			// state, then mark entries stable and the commit index applied
			if !IsEmptyHardState(prevHard) {
				if err := r.raftLog.storage.SetHardState(prevHard); err != nil {
					panic(err)
				}
			}

			if prevHard.CommitIndex != 0 {
				r.raftLog.appliedTo(prevHard.CommitIndex)
			}

			if hasPrevLastUnstableI {
				if err := r.raftLog.storage.Append(rd.Entries); err != nil {
					panic(err)
				}
				r.raftLog.stableTo(prevLastUnstableI, prevLastUnstableT)
				hasPrevLastUnstableI = false
			}

			advanceChan = nil
		}
	}
}

func (n *Node) Tick() {
	select {
	case n.tickChan <- struct{}{}:
	default:
	}
}

func (n *Node) Campaign(ctx context.Context) error {
	return n.step(ctx, Message{Type: MsgHup})
}

func (n *Node) Propose(ctx context.Context, data []byte) error {
	return n.step(ctx, Message{Type: MsgProp, Entries: []Entry{{Data: data}}})
}

func (n *Node) ProposeConfChange(ctx context.Context, cc ConfChange) error {
	data, _ := json.Marshal(cc)

	return n.step(ctx, Message{Type: MsgProp, Entries: []Entry{{Type: EntryConfChange, Data: data}}})
}

// ReadIndex submits a linearizable read request identified by rctx. Once a
// quorum confirms the leader, the read state is delivered through
// Ready.ReadStates.
func (n *Node) ReadIndex(ctx context.Context, rctx []byte) error {
	return n.step(ctx, Message{Type: MsgReadIndex, Entries: []Entry{{Data: rctx}}})
}

// ApplyConfChange applies a committed configuration change and returns the
// resulting cluster membership. Call it when applying an EntryConfChange
// entry from Ready.CommittedEntries.
func (n *Node) ApplyConfChange(cc ConfChange) ConfState {
	n.confChan <- cc
	return <-n.confStateChan
}

func (n *Node) Ready() <-chan Ready {
	return n.readyChan
}

func (n *Node) Advance() {
	n.advanceChan <- struct{}{}
}

func (n *Node) step(ctx context.Context, m Message) error {
	ch := n.recvChan
	if m.Type == MsgProp {
		ch = n.proc
	}

	select {
	case ch <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
