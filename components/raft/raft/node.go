package raft

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

var ErrStopped = errors.New("raft node is stopped")

type Node struct {
	stopped       chan struct{}
	done          chan struct{}
	stopOnce      *sync.Once
	proc          chan Message
	recvChan      chan Message
	confChan      chan ConfChange
	confStateChan chan ConfState
	readyChan     chan Ready
	advanceChan   chan struct{}
	tickChan      chan struct{}
}

func StartNode(conf *Config, peers []Peer) Node {
	r := newRaft(conf)
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
		stopped: make(chan struct{}), done: make(chan struct{}), stopOnce: &sync.Once{},
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
	defer close(n.done)
	var (
		readyChan   chan Ready
		advanceChan chan struct{}
		rd          Ready
		prevSoft    = r.softState()
		prevHard    = emptyHardState

		prevLastUnstableI, prevLastUnstableT uint64
		hasPrevLastUnstableI                 bool
	)

	for {
		if advanceChan != nil {
			readyChan = nil
		} else if rd = newReady(r, prevSoft, prevHard); rd.containsUpdates() {
			readyChan = n.readyChan
		} else {
			readyChan = nil
		}

		select {
		case <-n.stopped:
			return
		case m := <-n.proc:
			m.From = r.id
			r.Step(m)
		case m := <-n.recvChan:
			r.Step(m)
		case cc := <-n.confChan:
			state := r.applyConfChange(cc)
			select {
			case n.confStateChan <- state:
			case <-n.stopped:
				return
			}
		case <-n.tickChan:
			r.tick()
		case readyChan <- rd:
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

func (n *Node) ReadIndex(ctx context.Context, rctx []byte) error {
	return n.step(ctx, Message{Type: MsgReadIndex, Entries: []Entry{{Data: rctx}}})
}

func (n *Node) ApplyConfChange(cc ConfChange) ConfState {
	select {
	case n.confChan <- cc:
	case <-n.stopped:
		return ConfState{}
	}
	select {
	case state := <-n.confStateChan:
		return state
	case <-n.stopped:
		return ConfState{}
	}
}

func (n *Node) Ready() <-chan Ready {
	return n.readyChan
}

func (n *Node) Advance() {
	select {
	case n.advanceChan <- struct{}{}:
	case <-n.stopped:
	}
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
	case <-n.stopped:
		return ErrStopped
	}
}

func (n *Node) Stop() {
	n.stopOnce.Do(func() { close(n.stopped) })
	<-n.done
}
