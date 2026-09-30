package raft

type ProgressStateType int32

const (
	ProgressStateProbe     ProgressStateType = 0
	ProgressStateReplicate ProgressStateType = 1
)

type Progress struct {
	// Tracks a follower's replication state: confirmed match index and next index to send
	Match, Next uint64
	State       ProgressStateType
}

func (pr *Progress) maybeUpdate(n uint64) bool {
	var updated bool
	if pr.Match < n {
		pr.Match = n
		updated = true
	}
	if pr.Next < n+1 {
		pr.Next = n + 1
	}
	return updated
}

func (pr *Progress) mayDecreaseTo(logIndex, rejectHint uint64) bool {
	if pr.Next-1 != logIndex {
		return false
	}
	if pr.Next = min(logIndex, rejectHint+1); pr.Next < 1 {
		pr.Next = 1
	}
	return true
}
