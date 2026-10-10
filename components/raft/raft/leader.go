package raft

func (r *raft) becomeLeader() {
	if r.state == StateFollower {
		panic("invalid transition [follower -> leader]")
	}
	r.step = stepLeader
	r.reset(r.Term)
	r.tick = r.tickHeartbeat
	r.lead = r.id
	r.state = StateLeader

	lastIndex := r.raftLog.lastIndex()
	for id := range r.prs {
		if id == r.id {
			continue
		}
		r.prs[id] = &Progress{Next: lastIndex + 1}
	}

	r.appendEntry([]Entry{{Data: nil}}...)
}

func stepLeader(r *raft, m Message) {
	switch m.Type {
	case MsgBeat:
		r.broadcastHeartbeat()
		return
	case MsgProp:
		if len(m.Entries) == 0 {
			panic("propose entries can not be empty")
		}
		if _, ok := r.prs[r.id]; !ok {
			return
		}

		for i, e := range m.Entries {
			if e.Type == EntryConfChange {
				if r.pendingConf {
					m.Entries[i] = Entry{Type: EntryNormal}
				}
				r.pendingConf = true
			}
		}

		r.appendEntry(m.Entries...)

		r.broadcastAppend()
		return
	case MsgReadIndex:
		if _, ok := r.prs[r.id]; !ok {
			return
		}

		if len(r.prs) > 1 {
			if r.raftLog.zeroTermOnErrCompacted(r.raftLog.term(r.raftLog.commitIndex)) != r.Term {
				return
			}

			readIndex := r.raftLog.commitIndex
			ctx := m.Entries[0].Data
			if r.readOnly.addRequest(string(ctx), readIndex, m) {
				r.broadcastHeartbeatWithCtx(ctx)
			}
		} else {
			r.readStates = append(r.readStates, ReadState{
				Index:      r.raftLog.commitIndex,
				RequestCtx: m.Entries[0].Data,
			})
		}
		return
	}

	pr, ok := r.prs[m.From]
	if !ok {
		return
	}

	switch m.Type {
	case MsgAppResp:
		if m.Reject {
			if pr.mayDecreaseTo(m.LogIndex, m.RejectHint) {
				r.sendAppend(m.From)
			}
			return
		}

		if pr.maybeUpdate(m.LogIndex) && r.maybeCommit() {
			r.broadcastAppend()
		}

	case MsgHeartbeatResp:
		if len(r.readOnly.readIndexQueue) == 0 {
			return
		}

		acks := r.readOnly.recvAck(string(m.Context), m.From)
		if len(acks) < r.quorum() {
			return
		}

		for _, rs := range r.readOnly.advance(string(m.Context)) {
			req := rs.req
			if req.From == None || req.From == r.id {
				r.readStates = append(r.readStates, ReadState{
					Index:      rs.index,
					RequestCtx: req.Entries[0].Data,
				})
				continue
			}

			r.send(Message{
				To:       req.From,
				Type:     MsgReadIndexResp,
				LogIndex: rs.index,
				Entries:  req.Entries,
			})
		}
	}
}

func (r *raft) broadcastHeartbeat() {
	r.broadcastHeartbeatWithCtx(nil)
}

func (r *raft) broadcastHeartbeatWithCtx(ctx []byte) {
	for id := range r.prs {
		if id == r.id {
			continue
		}
		r.sendHeartbeat(id, ctx)
	}
}

func (r *raft) sendHeartbeat(id uint64, ctx []byte) {
	commit := min(r.raftLog.commitIndex, r.prs[id].Match)

	m := Message{
		To:          id,
		Type:        MsgHeartbeat,
		CommitIndex: commit,
		Context:     ctx,
	}

	r.send(m)
}

func (r *raft) broadcastAppend() {
	for id := range r.prs {
		if id == r.id {
			continue
		}
		r.sendAppend(id)
	}
}

func (r *raft) sendAppend(to uint64) {
	pr := r.prs[to]
	term, _ := r.raftLog.term(pr.Next - 1)
	entries, _ := r.raftLog.entries(pr.Next)
	m := Message{
		To:          to,
		Type:        MsgApp,
		LogTerm:     term,
		LogIndex:    pr.Next - 1,
		Entries:     entries,
		CommitIndex: r.raftLog.commitIndex,
	}
	r.send(m)
}
