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

func (r *raft) becomePreCandidate() {
	if r.state == StateLeader {
		panic("invalid transition leader -> pre-candidate")
	}
	r.step = stepCandidate
	r.tick = r.tickElection
	// Pre-vote does not change the term but starts a fresh election round
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
	// Candidate votes for itself
	r.Vote = r.id
	r.state = StateCandidate
}

// State machine handler for the candidate role
func stepCandidate(r *raft, m Message) {
	var voteRespType MessageType
	if r.state == StatePreCandidate {
		voteRespType = MsgPreVoteResp
	} else {
		voteRespType = MsgVoteResp
	}

	switch m.Type {
	case MsgProp:
		// Candidate does not handle write proposals
		return
	case MsgApp:
		// Revert to follower upon receiving append from a higher-term leader
		r.becomeFollower(r.Term, m.From)
		// Handle log replication request
		r.handleAppendEntries(m)
	case MsgHeartbeat:
		r.becomeFollower(m.Term, m.From)
		// Update commit index via heartbeat
		r.handleHeartbeat(m)
	case voteRespType:
		// Tally votes from peers
		granted := r.poll(m.From, !m.Reject)
		switch r.quorum() {
		// Granted votes reached quorum
		case granted:
			if r.state == StatePreCandidate {
				r.campaign(campaignElection)
			} else {
				r.becomeLeader()
				// Broadcast append to sync existing entries; a no-op entry for the current term was added in becomeLeader
				r.broadcastAppend()
			}
		// Rejected votes reached quorum
		case len(r.votes) - granted:
			// Revert to follower
			r.becomeFollower(r.Term, None)
		}
	}
}
