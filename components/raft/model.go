package raft

import "math"

const (
	None    uint64 = 0
	noLimit uint64 = math.MaxUint64
)

type EntryType int32

const (
	EntryNormal     EntryType = 0
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
	MsgHup           MessageType = 0
	MsgBeat          MessageType = 1
	MsgProp          MessageType = 2
	MsgApp           MessageType = 3
	MsgAppResp       MessageType = 4
	MsgVote          MessageType = 5
	MsgVoteResp      MessageType = 6
	MsgHeartbeat     MessageType = 7
	MsgHeartbeatResp MessageType = 8
	MsgReadIndex     MessageType = 9
	MsgReadIndexResp MessageType = 10
	MsgPreVote       MessageType = 11
	MsgPreVoteResp   MessageType = 12
)

type Message struct {
	Type        MessageType `json:"type"`
	To          uint64      `json:"to"`
	From        uint64      `json:"from"`
	Term        uint64      `json:"term"`
	LogTerm     uint64      `json:"logTerm"`
	LogIndex    uint64      `json:"logIndex"`
	Entries     []Entry     `json:"entries"`
	CommitIndex uint64      `json:"commitIndex"`
	Reject      bool        `json:"reject"`
	RejectHint  uint64      `json:"rejectHint"`
	Context     []byte      `json:"context"`
}

type StateType int32

const (
	StateFollower     StateType = 0
	StateCandidate    StateType = 1
	StateLeader       StateType = 2
	StatePreCandidate StateType = 3
)

type SoftState struct {
	Lead      uint64
	RaftState StateType
}

func (s *SoftState) equal(pre *SoftState) bool {
	return s.Lead == pre.Lead && s.RaftState == pre.RaftState
}

var emptyHardState HardState

type HardState struct {
	Term        uint64 `json:""`
	Vote        uint64 `json:"vote"`
	CommitIndex uint64 `json:"commitIndex"`
}

func isHardStateEqual(a, b HardState) bool {
	return a.Term == b.Term && a.Vote == b.Vote && a.CommitIndex == b.CommitIndex
}

type ConfState struct {
	Nodes []uint64
}

type Config struct {
	ID            uint64
	peers         []uint64
	Storage       Storage
	Applied       uint64
	PreVote       bool
	ElectionTick  int32
	HeartbeatTick int32
}

type Peer struct {
	ID      uint64
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
