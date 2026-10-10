package main

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/components/raft/raft"
)

type raftProxy struct {
	proposeC    <-chan string
	confChangeC <-chan raft.ConfChange
	commitC     chan<- *string
	id          uint64
	peers       []string

	node raft.Node

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

	go r.listen()
}

func (r *raftProxy) listen() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	go r.listenRequest()

	for {
		select {
		case <-ticker.C:
			r.node.Tick()

		case <-r.node.Ready():

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
