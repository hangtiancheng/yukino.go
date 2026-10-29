package domain

// CallbackRecord is a task callback record (cg_task_callback_record).
// task_type looks like "3:mix_video" or "5:mix_video" (no risk: prefix).
type CallbackRecord struct {
	ID               uint64
	UserID           int64
	ProjectID        string
	TaskStepID       int64
	TaskType         string
	TaskInput        string // JSON
	TaskOutput       string // JSON
	Status           string
	ProductionLineID string
	CallbackTime     int64
	CreateTime       int64
	// UpdateTime is the last change time (second granularity). The risk-governance poller uses it as the "update cursor" (cursor B)
	// to catch async risk hits: the auditing/running → fail UPDATE refreshes this timestamp.
	UpdateTime int64
}

// IsTerminal reports whether the state is terminal (done/fail).
func (r *CallbackRecord) IsTerminal() bool {
	return r.Status == ProjectStatusDone || r.Status == ProjectStatusFail
}
