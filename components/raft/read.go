package raft

type ReadState struct {
	Index      uint64
	RequestCtx []byte
}

type readIndexStatus struct {
	req   Message
	index uint64
	acks  map[uint64]struct{}
}

type readOnly struct {
	pendingReadIndex map[string]*readIndexStatus
	readIndexQueue   []string
}

func newReadOnly() *readOnly {
	return &readOnly{
		pendingReadIndex: make(map[string]*readIndexStatus),
	}
}

func (ro *readOnly) addRequest(ctx string, readIndex uint64, m Message) bool {
	if _, ok := ro.pendingReadIndex[ctx]; ok {
		return false
	}

	ro.pendingReadIndex[ctx] = &readIndexStatus{
		req:   m,
		index: readIndex,
		acks:  make(map[uint64]struct{}),
	}
	ro.readIndexQueue = append(ro.readIndexQueue, ctx)
	return true
}

func (ro *readOnly) recvAck(ctx string, from uint64) map[uint64]struct{} {
	rs, ok := ro.pendingReadIndex[ctx]
	if !ok {
		return nil
	}

	rs.acks[from] = struct{}{}
	return rs.acks
}

func (ro *readOnly) advance(ctx string) []*readIndexStatus {
	var (
		i     int
		found bool
	)
	rss := make([]*readIndexStatus, 0, len(ro.readIndexQueue))
	for _, queued := range ro.readIndexQueue {
		i++
		rs, ok := ro.pendingReadIndex[queued]
		if !ok {
			panic("cannot find corresponding read state from pending map")
		}

		rss = append(rss, rs)
		if queued == ctx {
			found = true
			break
		}
	}

	if found {
		ro.readIndexQueue = ro.readIndexQueue[i:]
		for _, rs := range rss {
			delete(ro.pendingReadIndex, string(rs.req.Entries[0].Data))
		}
		return rss
	}
	return nil
}
