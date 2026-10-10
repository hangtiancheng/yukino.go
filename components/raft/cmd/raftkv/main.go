package main

import "github.com/hangtiancheng/yukino.go/components/raft/raft"

func main() {
	proposeC := make(chan string)
	confChangeC := make(chan raft.ConfChange)

	commitC := newRaftProxy(1, []string{"node1"}, proposeC, confChangeC)
	kvStore := newKVStore(proposeC, commitC)

	s := newService(kvStore, proposeC, confChangeC)
	serveHttpApi(8091, s)
}
