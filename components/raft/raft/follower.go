package raft

func (r *raft) becomeFollower(term, lead uint64) {
	r.reset(term)
	r.step = stepFollower
	r.tick = r.tickElection
	r.lead = lead
	r.state = StateFollower
}

func stepFollower(r *raft, m Message) {
	switch m.Type {
	case MsgProp:
		if r.lead == None {
			return
		}

		m.To = r.lead
		r.send(m)

	case MsgApp:
		r.electionElapsed = 0
		r.lead = m.From
		r.handleAppendEntries(m)
	case MsgHeartbeat:
		r.electionElapsed = 0
		r.lead = m.From
		r.handleHeartbeat(m)
	case MsgReadIndex:
		if r.lead == None {
			return
		}

		m.To = r.lead
		r.send(m)

	case MsgReadIndexResp:
		r.readStates = append(r.readStates, ReadState{Index: m.LogIndex, RequestCtx: m.Entries[0].Data})
	}
}

func (r *raft) handleAppendEntries(m Message) {
	if mLastIndex, ok := r.raftLog.maybeAppend(m.LogIndex, m.LogTerm, m.CommitIndex, m.Entries...); ok {
		r.send(Message{To: m.From, Type: MsgAppResp, LogIndex: mLastIndex})
		return
	}

	r.send(Message{To: m.From, Type: MsgAppResp, LogIndex: m.LogIndex, Reject: true, RejectHint: r.raftLog.lastIndex()})
}

func (r *raft) handleHeartbeat(m Message) {
	r.raftLog.commitTo(min(m.CommitIndex, r.raftLog.lastIndex()))
	r.send(Message{To: m.From, Type: MsgHeartbeatResp, Context: m.Context})
}
