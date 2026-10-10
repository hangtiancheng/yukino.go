package raft

import (
	"math/rand"
	"slices"
	"sort"
)

type stepFunc func(*raft, Message)

type raft struct {
	id                        uint64
	Term                      uint64
	readStates                []ReadState
	raftLog                   *raftLog
	prs                       map[uint64]*Progress
	state                     StateType
	votes                     map[uint64]bool
	msgs                      []Message
	lead                      uint64
	pendingConf               bool
	readOnly                  *readOnly
	preVote                   bool
	tick                      func()
	step                      stepFunc
	Vote                      uint64
	checkQuorum               bool
	electionTimeout           int32
	randomizedElectionTimeout int32
	electionElapsed           int32
	heartbeatTimeout          int32
	heartbeatElapsed          int32
}

func newRaft(conf *Config) *raft {
	hs, cs, err := conf.Storage.InitialState()
	if err != nil {
		panic(err)
	}

	if len(conf.peers) == 0 {
		conf.peers = cs.Nodes
	}

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

	for _, peer := range conf.peers {
		r.prs[peer] = &Progress{Next: 1}
	}

	r.raftLog.appliedTo(conf.Applied)

	if hs.CommitIndex > r.raftLog.commitIndex {
		r.raftLog.commitIndex = hs.CommitIndex
	}

	term := hs.Term
	if term == 0 {
		term = 1
	}
	r.becomeFollower(term, None)

	return &r
}

func (r *raft) Step(m Message) error {
	switch {
	case m.Term == 0:
	case m.Term > r.Term:
		lead := m.From
		if m.Type == MsgVote || m.Type == MsgPreVote {
			lead = None
		}

		if m.Type != MsgPreVote && (m.Type != MsgPreVoteResp || m.Reject) {
			r.becomeFollower(m.Term, lead)
		}

	case m.Term < r.Term:
		if r.checkQuorum && (m.Type == MsgHeartbeat || m.Type == MsgApp) {
			r.send(Message{To: m.From, Type: MsgAppResp})
		}
		return nil
	}

	switch m.Type {
	case MsgHup:
		if r.state == StateLeader {
			break
		}
		if !r.promotable(r.id) {
			break
		}
		entries, err := r.raftLog.slice(r.raftLog.applyIndex+1, r.raftLog.commitIndex+1)
		if err != nil {
			panic(err)
		}

		if n := numOfPendingConf(entries); n > 0 {
			break
		}

		if r.preVote {
			r.campaign(campaignPreElection)
			break
		}
		r.campaign(campaignElection)

	case MsgVote, MsgPreVote:
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
	r.votes = make(map[uint64]bool)
	r.pendingConf = false
	r.resetRandomizedElectionTimeout()
}

func (r *raft) resetRandomizedElectionTimeout() {
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

	if id == r.id {
		r.becomeFollower(r.Term, None)
	}
}

func (r *raft) applyConfChange(cc ConfChange) ConfState {
	if cc.NodeID != None {
		switch cc.Type {
		case ConfChangeAddNode:
			r.addNode(cc.NodeID)
		case ConfChangeRemoveNode:
			r.removeNode(cc.NodeID)
		case ConfChangeUpdateNode:
			r.addNode(cc.NodeID)
		}
	}

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
	var (
		term    uint64
		msgType MessageType
	)

	if typ == campaignPreElection {
		r.becomePreCandidate()
		term = r.Term + 1
		msgType = MsgPreVote
	} else {
		r.becomeCandidate()
		term = r.Term
		msgType = MsgVote
	}

	if r.quorum() == r.poll(r.id, true) {
		if typ == campaignPreElection {
			r.campaign(campaignElection)
		} else {
			r.becomeLeader()
		}
		return
	}

	for id := range r.prs {
		if id == r.id {
			continue
		}

		r.send(Message{Term: term, To: id, Type: msgType, LogTerm: r.raftLog.lastTerm(), LogIndex: r.raftLog.lastIndex()})
	}
}

func (r *raft) poll(id uint64, v bool) int {
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
	r.raftLog.append(es...)
	r.prs[r.id].maybeUpdate(r.raftLog.lastIndex())

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
