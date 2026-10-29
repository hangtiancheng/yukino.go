package domain

// AuditRecord status constants (aligned with the project state machine).
const (
	StatusAuditing = "auditing" // under audit (async risk inserts as auditing)
	StatusRunning  = "running"  // under audit (sync risk semantics reuse)
	StatusDone     = "done"     // audit passed (terminal)
	StatusFail     = "fail"     // audit rejected (terminal)
)

// AuditRecord is the risk-audit record entity (cg_risk_audit_record).
type AuditRecord struct {
	ID          uint64
	UserID      int64
	ProjectID   string
	TaskStepID  int64
	TaskType    string // "risk:{role}:{type}:{stage}" or "risk:async:{subProduct}"
	TaskInput   string
	Status      string
	CallbackURL string
	CreateTime  int64
	UpdateTime  int64
}

// IsTerminal reports whether the state is terminal (done/fail).
func (r *AuditRecord) IsTerminal() bool {
	return r.Status == StatusDone || r.Status == StatusFail
}
