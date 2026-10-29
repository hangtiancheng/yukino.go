package domain

// PointsAccount is the user points-account aggregate root.
// Balance = total credits (refund/return flow_type=2) - total deductions (flow_type=1 AND status=1).
type PointsAccount struct {
	UserID  int64
	Balance int
}

// CanAfford reports whether the balance can cover cost.
func (a *PointsAccount) CanAfford(cost int) bool { return a.Balance >= cost }
