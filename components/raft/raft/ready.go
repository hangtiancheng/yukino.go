package raft

type Ready struct {
	SoftState *SoftState

	HardState HardState

	ReadStates []ReadState

	Entries []Entry

	CommittedEntries []Entry

	Message []Message
}

func newReady(r *raft, preSoft *SoftState, preHard HardState) Ready {
	rd := Ready{
		Entries:          r.raftLog.unstableEntries(),
		CommittedEntries: r.raftLog.nextEntries(),
		Message:          r.msgs,
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
