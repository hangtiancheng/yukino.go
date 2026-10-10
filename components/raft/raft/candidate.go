package raft

func (r *raft) becomePreCandidate() {
	if r.state == StateLeader {
		panic("invalid transition leader -> pre-candidate")
	}
	r.step = stepCandidate
	r.tick = r.tickElection
	r.votes = make(map[uint64]bool)
	r.state = StatePreCandidate
}

func (r *raft) becomeCandidate() {
	if r.state == StateLeader {
		panic("invalid transition leader -> candidate")
	}
	r.step = stepCandidate
	r.reset(r.Term + 1)
	r.tick = r.tickElection
	r.Vote = r.id
	r.state = StateCandidate
}

func stepCandidate(r *raft, m Message) {
	var voteRespType MessageType
	if r.state == StatePreCandidate {
		voteRespType = MsgPreVoteResp
	} else {
		voteRespType = MsgVoteResp
	}

	switch m.Type {
	case MsgProp:
		return
	case MsgApp:
		r.becomeFollower(r.Term, m.From)
		r.handleAppendEntries(m)
	case MsgHeartbeat:
		r.becomeFollower(m.Term, m.From)
		r.handleHeartbeat(m)
	case voteRespType:
		granted := r.poll(m.From, !m.Reject)
		switch r.quorum() {
		case granted:
			if r.state == StatePreCandidate {
				r.campaign(campaignElection)
			} else {
				r.becomeLeader()
				r.broadcastAppend()
			}
		case len(r.votes) - granted:
			r.becomeFollower(r.Term, None)
		}
	}
}
