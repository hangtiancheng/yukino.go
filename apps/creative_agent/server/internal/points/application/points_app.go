package application

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/domain"
)

// PointsApp 积分应用服务：编排扣退/余额/流水查询。
type PointsApp struct {
	repo domain.PointsRepository
}

// NewPointsApp 创建 PointsApp。
func NewPointsApp(repo domain.PointsRepository) *PointsApp {
	return &PointsApp{repo: repo}
}

// GetBalance 查用户余额。
func (a *PointsApp) GetBalance(ctx context.Context, userID int64) (int, error) {
	return a.repo.GetBalance(ctx, userID)
}

// Deduct 扣积分（幂等：order_id UNIQUE 防重复扣）。
// orderID 由调用方保证唯一（如 {projectID}_{retryNum}）。
func (a *PointsApp) Deduct(ctx context.Context, userID int64, projectID, orderID, projectType string, amount int) (uint64, error) {
	return a.repo.InsertConsume(ctx, &domain.PointsFlow{
		UserID: userID, ProjectID: projectID, OrderID: orderID,
		Amount: amount, SceneDesc: domain.ConsumeTitle(projectType),
	})
}

// Refund 退积分（幂等：已退过返回 nil 不报错）。
func (a *PointsApp) Refund(ctx context.Context, orderID string, userID int64, projectType string) error {
	return a.repo.RefundByOrderID(ctx, orderID, userID, domain.BackTitle(projectType))
}

// ListByProject 反查某作品扣退记录（对账）。
func (a *PointsApp) ListByProject(ctx context.Context, projectID string) ([]*domain.PointsFlow, error) {
	return a.repo.ListByProject(ctx, projectID)
}
