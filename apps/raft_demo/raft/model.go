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

import "math"

const (
	None    uint64 = 0
	noLimit uint64 = math.MaxUint64
)

type EntryType int32

const (
	// Normal log entry
	EntryNormal EntryType = 0
	// Configuration change entry
	EntryConfChange EntryType = 1
)

type Entry struct {
	Term  uint64    `json:"term"`
	Index uint64    `json:"index"`
	Type  EntryType `json:"type"`
	Data  []byte    `json:"data"`
}

type MessageType int32

const (
	// Triggers local node election
	MsgHup MessageType = 0
	// Triggers leader to broadcast heartbeats
	MsgBeat MessageType = 1
	// Client proposal submitted to raft
	MsgProp MessageType = 2
	// Leader replicates entries to followers
	MsgApp MessageType = 3
	// Follower responds to leader's replication request
	MsgAppResp MessageType = 4
	// Vote request
	MsgVote     MessageType = 5
	MsgVoteResp MessageType = 6
	// Heartbeat
	MsgHeartbeat     MessageType = 7
	MsgHeartbeatResp MessageType = 8
	// Linearizable read
	MsgReadIndex     MessageType = 9
	MsgReadIndexResp MessageType = 10
	// Pre-vote
	MsgPreVote     MessageType = 11
	MsgPreVoteResp MessageType = 12
)

type Message struct {
	Type MessageType `json:"type"`
	To   uint64      `json:"to"`
	From uint64      `json:"from"`
	// Current term
	Term uint64 `json:"term"`
	// Term of the preceding log entry
	LogTerm  uint64 `json:"logTerm"`
	LogIndex uint64 `json:"logIndex"`
	// Log entries to replicate
	Entries []Entry `json:"entries"`
	// Leader's commit index
	CommitIndex uint64 `json:"commitIndex"`
	// Whether the request is rejected
	Reject bool `json:"reject"`
	// Hint index for rejection (follower's last log index)
	RejectHint uint64 `json:"rejectHint"`
	// Arbitrary context data
	Context []byte `json:"context"`
}

type StateType int32

const (
	// Follower
	StateFollower StateType = 0
	// Candidate
	StateCandidate StateType = 1
	// Leader
	StateLeader StateType = 2
	// Pre-candidate
	StatePreCandidate StateType = 3
)

type SoftState struct {
	// Current cluster leader ID
	Lead uint64
	// Current node state
	RaftState StateType
}

func (s *SoftState) equal(pre *SoftState) bool {
	return s.Lead == pre.Lead && s.RaftState == pre.RaftState
}

var emptyHardState HardState

type HardState struct {
	// Current term
	Term uint64 `json:""`
	// Candidate voted for in the current term
	Vote uint64 `json:"vote"`
	// Committed log index
	CommitIndex uint64 `json:"commitIndex"`
}

func isHardStateEqual(a, b HardState) bool {
	return a.Term == b.Term && a.Vote == b.Vote && a.CommitIndex == b.CommitIndex
}

type ConfState struct {
	// Cluster node IDs
	Nodes []uint64
}

type Config struct {
	// Local node ID
	ID uint64
	// Peer node IDs
	peers []uint64
	// Persistent storage interface
	Storage Storage
	// Applied log index
	Applied uint64
	// Whether pre-vote is enabled
	PreVote bool
	// Election timeout tick for followers
	ElectionTick int32
	// Heartbeat tick for the leader
	HeartbeatTick int32
}

type Peer struct {
	// Node ID
	ID uint64
	// Context data
	Context []byte
}

type CampaignType string

const (
	campaignPreElection CampaignType = "prev"
	campaignElection    CampaignType = "nor"
)

type ConfChangeType int32

const (
	ConfChangeAddNode    ConfChangeType = 0
	ConfChangeRemoveNode ConfChangeType = 1
	ConfChangeUpdateNode ConfChangeType = 2
)

type ConfChange struct {
	ID      uint64         `json:"id"`
	Type    ConfChangeType `json:"type"`
	NodeID  uint64         `json:"nodeID"`
	Context []byte         `json:"context"`
}
