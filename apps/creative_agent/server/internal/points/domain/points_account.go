package domain

// PointsAccount 用户积分账户聚合根。
// 余额 = 总入账(退/返还 flow_type=2) - 总扣分(flow_type=1 AND status=1)。
type PointsAccount struct {
	UserID  int64
	Balance int
}

// CanAfford 余额是否足够支付 cost。
func (a *PointsAccount) CanAfford(cost int) bool { return a.Balance >= cost }
