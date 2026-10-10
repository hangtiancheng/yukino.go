package main

import (
	"io"
	"net/http"
	"strconv"

	"github.com/hangtiancheng/yukino.go/components/raft/raft"
)

type service struct {
	proposeC    chan<- string
	confChangeC chan<- raft.ConfChange
	kvStore     *kvStore
}

func newService(kvStore *kvStore, proposeC chan<- string, confChangeC chan<- raft.ConfChange) *service {
	return &service{
		proposeC:    proposeC,
		confChangeC: confChangeC,
		kvStore:     kvStore,
	}
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	url := r.RequestURI

	switch r.Method {
	case http.MethodPut:
		v, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request body: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.kvStore.Propose(url, string(v))

	case http.MethodPost:
		v, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request body: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if len(url) < 2 {
			http.Error(w, "missing node id", http.StatusBadRequest)
			return
		}
		nodeID, err := strconv.ParseUint(url[1:], 0, 64)
		if err != nil {
			http.Error(w, "invalid node id: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.confChangeC <- raft.ConfChange{
			NodeID:  nodeID,
			Type:    raft.ConfChangeAddNode,
			Context: v,
		}

	}

}

func serveHttpApi(port int, s *service) {
	srv := http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: s,
	}

	if err := srv.ListenAndServe(); err != nil {
		panic(err)
	}
}
