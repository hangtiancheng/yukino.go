package domain

// FlowType 积分流水类型。
const (
	FlowTypeConsume = 1 // 扣积分
	FlowTypeRefund  = 2 // 退积分
)

// FlowStatus 流水状态。
const (
	FlowStatusValid      = 1 // 有效
	FlowStatusRolledBack = 0 // 已回退（对应扣分流水被退积分标记）
)

// PointsFlow 积分流水实体（cg_points_flow）。
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

// IsConsume / IsRefund 便捷判断。
func (f *PointsFlow) IsConsume() bool { return f.FlowType == FlowTypeConsume }
func (f *PointsFlow) IsRefund() bool  { return f.FlowType == FlowTypeRefund }
