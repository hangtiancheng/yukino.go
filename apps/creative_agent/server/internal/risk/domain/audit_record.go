package domain

// AuditRecord 状态常量（对齐 project 状态机）。
const (
	StatusAuditing = "auditing" // 送审中（异步风控插入即 auditing）
	StatusRunning  = "running"  // 送审中（同步风控语义复用）
	StatusDone     = "done"     // 审核通过（终态）
	StatusFail     = "fail"     // 审核不通过（终态）
)

// AuditRecord 风控审计记录实体（cg_risk_audit_record）。
type AuditRecord struct {
	ID          uint64
	UserID      int64
	ProjectID   string
	TaskStepID  int64
	TaskType    string // "risk:{role}:{type}:{stage}" 或 "risk:async:{subProduct}"
	TaskInput   string
	Status      string
	CallbackURL string
	CreateTime  int64
	UpdateTime  int64
}

// IsTerminal 是否终态（done/fail）。
func (r *AuditRecord) IsTerminal() bool {
	return r.Status == StatusDone || r.Status == StatusFail
}
