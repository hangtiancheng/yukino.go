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

import (
	"math/rand"
	"slices"
	"sort"
)

type stepFunc func(*raft, Message)

type raft struct {
	// Local node ID
	id uint64
	// Current term
	Term uint64
	// Linearizable read states
	readStates []ReadState
	// Raft log module
	raftLog *raftLog
	// Replication progress for each peer
	prs map[uint64]*Progress
	// Current node state
	state StateType
	// Votes received from peers
	votes map[uint64]bool
	// Outbound message queue
	msgs []Message
	// Current leader ID
	lead uint64
	// Indicates pending unapplied configuration entries
	pendingConf bool
	// Global read-only request tracker
	readOnly *readOnly
	// Whether pre-vote is enabled
	preVote bool
	// Tick handler invoked on timer expiry; behavior varies by role
	tick func()
	// Step function handling incoming messages; behavior varies by role
	step stepFunc
	// Candidate voted for in the current term
	Vote uint64
	// Whether quorum checking is enabled
	checkQuorum bool
	// Election timeout in ticks
	electionTimeout int32
	// Randomized election timeout in ticks
	randomizedElectionTimeout int32
	// Election elapsed tick counter
	electionElapsed int32
	// Heartbeat timeout in ticks
	heartbeatTimeout int32
	// Heartbeat elapsed tick counter
	heartbeatElapsed int32
}

func newRaft(conf *Config) *raft {
	// Retrieve initial state from storage
	hs, cs, err := conf.Storage.InitialState()
	if err != nil {
		panic(err)
	}

	// Restore cluster membership from storage when no initial peers are configured
	if len(conf.peers) == 0 {
		conf.peers = cs.Nodes
	}

	// Initialize raft instance from configuration
	r := raft{
		id:               conf.ID,
		lead:             None,
		raftLog:          newRaftLog(conf.Storage),
		electionTimeout:  conf.ElectionTick,
		heartbeatTimeout: conf.HeartbeatTick,
		preVote:          conf.PreVote,
		readOnly:         newReadOnly(),
		prs:              make(map[uint64]*Progress),
		votes:            make(map[uint64]bool),
	}

	// Register peers in the progress map
	for _, peer := range conf.peers {
		r.prs[peer] = &Progress{Next: 1}
	}

	// Restore applied index
	r.raftLog.appliedTo(conf.Applied)

	// Restore the committed index from the persisted hard state
	if hs.CommitIndex > r.raftLog.commitIndex {
		r.raftLog.commitIndex = hs.CommitIndex
	}

	// Start as a follower, restoring the persisted term when available
	term := hs.Term
	if term == 0 {
		term = 1
	}
	r.becomeFollower(term, None)

	return &r
}

func (r *raft) Step(m Message) error {
	// Dispatch by term comparison
	switch {
	// Local message, pass through
	case m.Term == 0:
	case m.Term > r.Term:
		lead := m.From
		// Message has a higher term
		if m.Type == MsgVote || m.Type == MsgPreVote {
			lead = None
		}

		if m.Type != MsgPreVote && (m.Type != MsgPreVoteResp || m.Reject) {
			r.becomeFollower(m.Term, lead)
		}

	case m.Term < r.Term:
		// Message has a lower term
		// For heartbeat/append from a stale leader, respond with current term
		if r.checkQuorum && (m.Type == MsgHeartbeat || m.Type == MsgApp) {
			r.send(Message{To: m.From, Type: MsgAppResp})
		}
		// Discard messages with a lower term
		return nil
	}

	// Dispatch by message type
	switch m.Type {
	// Trigger local election
	case MsgHup:
		// Already the leader, skip
		if r.state == StateLeader {
			break
		}
		// A node that is no longer a cluster member must not campaign:
		// becoming leader with an empty progress map would nil-deref its own
		// Progress in appendEntry
		if !r.promotable(r.id) {
			break
		}
		// Cannot campaign with unapplied configuration changes
		entries, err := r.raftLog.slice(r.raftLog.applyIndex+1, r.raftLog.commitIndex+1)
		if err != nil {
			panic(err)
		}

		if n := numOfPendingConf(entries); n > 0 {
			break
		}

		// Start election
		if r.preVote {
			// Start pre-vote election
			r.campaign(campaignPreElection)
			break
		}
		r.campaign(campaignElection)

	// Handle vote request
	case MsgVote, MsgPreVote:
		// Received a vote request with term >= current term
		// Grant vote if the candidate's log is up-to-date and one of:
		// (1) haven't voted yet, (2) candidate's term is higher, (3) already voted for this candidate
		if r.raftLog.isUpToDate(m.LogIndex, m.LogTerm) && (r.Vote == None || m.Term > r.Term || m.From == r.Vote) {
			if m.Type == MsgVote {
				r.Vote = m.From
				r.send(Message{Type: MsgVoteResp, To: m.From})
				break
			}
			r.send(Message{Type: MsgPreVoteResp, To: m.From})
			break
		}
		if m.Type == MsgVote {
			r.send(Message{Type: MsgVoteResp, To: m.From, Reject: true})
			break
		}
		r.send(Message{Type: MsgPreVoteResp, To: m.From, Reject: true})

	// Delegate to role-specific state machine handler
	default:
		r.step(r, m)
	}

	return nil
}

func (r *raft) reset(term uint64) {
	if r.Term != term {
		r.Term = term
		r.Vote = None
	}
	r.lead = None
	r.electionElapsed = 0
	r.heartbeatElapsed = 0
	// A new term starts with a clean election state and no pending
	// configuration change
	r.votes = make(map[uint64]bool)
	r.pendingConf = false
	// Reset randomized election timeout
	r.resetRandomizedElectionTimeout()
}

func (r *raft) resetRandomizedElectionTimeout() {
	// Guard against a zero/negative election tick so rand.Intn cannot panic
	n := r.electionTimeout
	if n <= 0 {
		n = 1
	}
	r.randomizedElectionTimeout = n + int32(rand.Intn(int(n)))
}

func (r *raft) softState() *SoftState {
	return &SoftState{Lead: r.lead, RaftState: r.state}
}

func (r *raft) hardState() HardState {
	return HardState{
		Term:        r.Term,
		CommitIndex: r.raftLog.commitIndex,
		Vote:        r.Vote,
	}
}

func (r *raft) addNode(id uint64) {
	if _, ok := r.prs[id]; ok {
		return
	}
	r.prs[id] = &Progress{Match: 0, Next: r.raftLog.lastIndex() + 1}
}

func (r *raft) removeNode(id uint64) {
	if _, ok := r.prs[id]; !ok {
		return
	}
	delete(r.prs, id)

	// A node that has been removed from the cluster steps down
	if id == r.id {
		r.becomeFollower(r.Term, None)
	}
}

// applyConfChange applies a committed configuration change and returns the
// resulting cluster membership.
func (r *raft) applyConfChange(cc ConfChange) ConfState {
	if cc.NodeID != None {
		switch cc.Type {
		case ConfChangeAddNode:
			r.addNode(cc.NodeID)
		case ConfChangeRemoveNode:
			r.removeNode(cc.NodeID)
		case ConfChangeUpdateNode:
			// Node attributes are not tracked; adding an existing node is a no-op
			r.addNode(cc.NodeID)
		}
	}

	// The conf change has been applied; the next one may be proposed
	r.pendingConf = false
	return ConfState{Nodes: r.nodes()}
}

func (r *raft) nodes() []uint64 {
	nodes := make([]uint64, 0, len(r.prs))
	for id := range r.prs {
		nodes = append(nodes, id)
	}
	slices.Sort(nodes)
	return nodes
}

func (r *raft) send(m Message) {
	if m.From == None {
		m.From = r.id
	}
	if m.Type != MsgProp && m.Type != MsgReadIndex {
		m.Term = r.Term
	}

	r.msgs = append(r.msgs, m)
}

func (r *raft) campaign(typ CampaignType) {
	// Campaign consists of pre-vote and vote phases
	var (
		term    uint64
		msgType MessageType
	)

	if typ == campaignPreElection {
		// Pre-vote does not increment the term
		r.becomePreCandidate()
		term = r.Term + 1
		msgType = MsgPreVote
	} else {
		// Election increments the term
		r.becomeCandidate()
		term = r.Term
		msgType = MsgVote
	}

	// Vote for self first, then check if quorum is already reached
	if r.quorum() == r.poll(r.id, true) {
		if typ == campaignPreElection {
			r.campaign(campaignElection)
		} else {
			r.becomeLeader()
		}
		return
	}

	// Request votes from all peers
	for id := range r.prs {
		// Skip self
		if id == r.id {
			continue
		}

		r.send(Message{Term: term, To: id, Type: msgType, LogTerm: r.raftLog.lastTerm(), LogIndex: r.raftLog.lastIndex()})
	}
}

func (r *raft) poll(id uint64, v bool) int {
	// Record vote if not already tracked
	if _, ok := r.votes[id]; !ok {
		r.votes[id] = v
	}
	var granted int
	for _, vv := range r.votes {
		if vv {
			granted++
		}
	}
	return granted
}

func (r *raft) quorum() int {
	return len(r.prs)>>1 + 1
}

func (r *raft) tickElection() {
	r.electionElapsed++

	if r.promotable(r.id) && r.pastElectionTimeout() {
		r.electionElapsed = 0
		r.Step(Message{From: r.id, Type: MsgHup})
	}
}

func (r *raft) tickHeartbeat() {
	if r.state != StateLeader {
		return
	}

	r.heartbeatElapsed++
	if r.heartbeatElapsed >= r.heartbeatTimeout {
		r.heartbeatElapsed = 0
		r.Step(Message{From: r.id, Type: MsgBeat})
	}
}

// Checks whether the node is still a cluster member
func (r *raft) promotable(id uint64) bool {
	_, ok := r.prs[id]
	return ok
}

func (r *raft) pastElectionTimeout() bool {
	return r.electionElapsed >= r.randomizedElectionTimeout
}

func (r *raft) appendEntry(es ...Entry) {
	lastIndex := r.raftLog.lastIndex()
	for i := range es {
		es[i].Term = r.Term
		es[i].Index = lastIndex + uint64(i) + 1
	}
	// Assign term and index to new entries
	r.raftLog.append(es...)
	r.prs[r.id].maybeUpdate(r.raftLog.lastIndex())

	// The leader's own progress may already satisfy the quorum (e.g. a
	// single-member cluster)
	r.maybeCommit()
}

func (r *raft) maybeCommit() bool {
	matches := make(uint64Slice, 0, len(r.prs))
	for id := range r.prs {
		matches = append(matches, r.prs[id].Match)
	}

	sort.Sort(sort.Reverse(matches))
	mid := matches[r.quorum()-1]

	return r.raftLog.maybeCommit(mid, r.Term)
}
