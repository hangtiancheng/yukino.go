package raft

func (r *raft) becomeFollower(term, lead uint64) {
	r.reset(term)
	r.step = stepFollower
	r.tick = r.tickElection
	r.lead = lead
	r.state = StateFollower
}

// State machine handler for the follower role
func stepFollower(r *raft, m Message) {
	switch m.Type {
	case MsgProp:
		// Ignore if no leader is known
		if r.lead == None {
			return
		}

		// Forward to the known leader
		m.To = r.lead
		r.send(m)

	case MsgApp:
		// Handle log replication request
		// Reset election timer upon receiving leader's append request
		r.electionElapsed = 0
		r.lead = m.From
		r.handleAppendEntries(m)
	case MsgHeartbeat:
		// Handle heartbeat request
		// Reset election timer upon receiving leader's heartbeat
		r.electionElapsed = 0
		r.lead = m.From
		r.handleHeartbeat(m)
	case MsgReadIndex:
		// Handle linearizable read request
		// Ignore if no leader is known
		if r.lead == None {
			return
		}

		// Forward to the leader
		m.To = r.lead
		r.send(m)

	case MsgReadIndexResp:
		// Handle read index response
		r.readStates = append(r.readStates, ReadState{Index: m.LogIndex, RequestCtx: m.Entries[0].Data})
	}
}

func (r *raft) handleAppendEntries(m Message) {
	// Attempt to append; on success the local log matches the leader's log
	// through the last appended entry
	if mLastIndex, ok := r.raftLog.maybeAppend(m.LogIndex, m.LogTerm, m.CommitIndex, m.Entries...); ok {
		r.send(Message{To: m.From, Type: MsgAppResp, LogIndex: mLastIndex})
		return
	}

	// Append failed, send rejection with hint
	r.send(Message{To: m.From, Type: MsgAppResp, LogIndex: m.LogIndex, Reject: true, RejectHint: r.raftLog.lastIndex()})
}

func (r *raft) handleHeartbeat(m Message) {
	// Clamp to the local last index: a reordered or stale heartbeat may carry
	// a commit index the local log has not replicated yet, and commitTo would
	// panic on it
	r.raftLog.commitTo(min(m.CommitIndex, r.raftLog.lastIndex()))
	r.send(Message{To: m.From, Type: MsgHeartbeatResp, Context: m.Context})
}
