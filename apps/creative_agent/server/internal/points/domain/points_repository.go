package domain

import (
	"context"
	"errors"
)

// ErrAlreadyRefunded 表示该订单已退过积分（幂等拦截）。
var ErrAlreadyRefunded = errors.New("already refunded")

// PointsRepository 积分流水仓储接口（domain 定义，infra 实现）。
type PointsRepository interface {
	// GetBalance 计算用户当前余额。
	GetBalance(ctx context.Context, userID int64) (int, error)
	// InsertConsume 写扣积分流水。order_id UNIQUE 保证同一订单不重复扣。
	InsertConsume(ctx context.Context, flow *PointsFlow) (uint64, error)
	// InsertRefund 写退积分流水，同时把对应扣分流水标记为已回退。
	// 已退过返回 ErrAlreadyRefunded。
	InsertRefund(ctx context.Context, flow *PointsFlow) (uint64, error)
	// RefundByOrderID 按订单号幂等退积分：已退过返回 nil。
	RefundByOrderID(ctx context.Context, orderID string, userID int64, title string) error
	// ListByProject 反查某作品的所有扣退记录（对账）。
	ListByProject(ctx context.Context, projectID string) ([]*PointsFlow, error)
}
