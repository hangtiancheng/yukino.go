package raft

type Ready struct {
	// Soft state (leader ID, node role)
	SoftState *SoftState

	// Hard state (term, vote, commit index)
	HardState HardState

	// Linearizable read states
	ReadStates []ReadState

	// Unstable entries to persist before sending messages
	Entries []Entry

	// Committed entries to apply to the state machine
	CommittedEntries []Entry

	// Messages to send
	Message []Message
}

func newReady(r *raft, preSoft *SoftState, preHard HardState) Ready {
	rd := Ready{
		// Unstable entries requiring persistence
		Entries: r.raftLog.unstableEntries(),
		// Committed entries ready for application
		CommittedEntries: r.raftLog.nextEntries(),
		// Pending messages
		Message: r.msgs,
	}
	if soft := r.softState(); !soft.equal(preSoft) {
		rd.SoftState = soft
	}
	if hard := r.hardState(); !isHardStateEqual(hard, preHard) {
		rd.HardState = hard
	}
	if len(r.readStates) != 0 {
		rd.ReadStates = r.readStates
	}
	return rd
}

func (rd Ready) containsUpdates() bool {
	return rd.SoftState != nil || !IsEmptyHardState(rd.HardState) ||
		len(rd.Entries) > 0 || len(rd.CommittedEntries) > 0 ||
		len(rd.Message) > 0 || len(rd.ReadStates) > 0
}

func IsEmptyHardState(h HardState) bool {
	return isHardStateEqual(h, emptyHardState)
}
