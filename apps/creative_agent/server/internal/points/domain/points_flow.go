package domain

// FlowType is the points-ledger entry type.
const (
	FlowTypeConsume = 1 // points deduction
	FlowTypeRefund  = 2 // points refund
)

// FlowStatus is the ledger entry status.
const (
	FlowStatusValid      = 1 // valid
	FlowStatusRolledBack = 0 // rolled back (the deduction entry marked refunded)
)

// PointsFlow is the points-ledger entity (cg_points_flow).
type PointsFlow struct {
	ID         uint64
	UserID     int64
	ProjectID  string
	OrderID    string
	FlowType   int
	Amount     int
	SceneDesc  string
	Status     int
	CreateTime int64
}

// IsConsume / IsRefund convenience checks.
func (f *PointsFlow) IsConsume() bool { return f.FlowType == FlowTypeConsume }
func (f *PointsFlow) IsRefund() bool  { return f.FlowType == FlowTypeRefund }
