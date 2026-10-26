package service

import (
	"context"
	"encoding/json"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// CreateApp 是 create 用例的应用服务：扣积分 + 建作品（幂等）。
//
// ⚠️ 核心设计（难点 2：钱不能错）：刻意不用数据库事务。
// 扣积分（写本地积分流水）与插作品是两个独立写操作，模拟原项目"外部积分 RPC + 本地 DB"
// 的跨进程场景。用应用层补偿替代事务：写流水 → 插作品 → 失败则写退款流水"回滚"。
type CreateApp struct {
	projects domain.ProjectRepository
	points   PointsPort
}

// NewCreateApp 创建 create 应用服务。
func NewCreateApp(projects domain.ProjectRepository, points PointsPort) *CreateApp {
	return &CreateApp{
		projects: projects,
		points:   points,
	}
}

// HandleCreate 是 /inner/creation/create 的入口：扣积分 + 建作品（幂等）。
//
// 幂等三层保障：
//  1. 入口层：session 下已有 project 直接返回 success（重复调用不重复扣分）
//  2. 数据层：order_id UNIQUE 索引防止同一笔订单重复扣
//  3. 补偿层：插作品失败 → 写退积分流水（不丢钱）
func (a *CreateApp) HandleCreate(ctx context.Context, req *contracts.CreateReq) (*contracts.CreateResp, error) {
	// 0. 幂等检查：session 下已有 project → 直接返回 success
	existing, err := a.projects.GetBySessionID(ctx, req.SessionID, req.UserID)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return contracts.CreateSuccessResp(existing[0].PointsCost, 0), nil
	}

	// 1. 计算费用
	cost := domain.CalculateCost(req.ProjectType, req.ProjectInput)

	// 2. 查余额（本地积分流水表：sum(扣积分) - sum(退积分)）
	balance, err := a.points.GetBalance(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if balance < cost {
		return contracts.CreateInsufficientResp(cost, balance), nil
	}

	// 3. 写积分流水：扣积分（orderID 作幂等键，UNIQUE 约束防重复扣）
	orderID := buildOrderID(req.SessionID, 1) // {projectID}_{retryNum}，首次扣分 retryNum=1
	_, err = a.points.Deduct(ctx, req.UserID, req.SessionID, orderID, req.ProjectType, cost)
	if err != nil {
		return nil, err
	}

	// 4. 插作品（本地 DB，与步骤 3 不在同一事务！）
	project := &domain.Project{
		UserID:        req.UserID,
		SessionID:     req.SessionID,
		ProjectID:     req.SessionID,
		ProjectType:   req.ProjectType,
		ProjectInput:  marshalProjectInput(req.ProjectInput),
		Title:         req.Title,
		Status:        domain.ProjectStatusRunning,
		PointsStatus:  domain.PointStatusUsed, // 已扣
		PointsCost:    cost,
		PointsOrderID: orderID, // {session}_1
		RetryNum:      1,       // 第 1 次扣分已消耗（orderID 用了 slot 1），首次重试从 _2 开始
	}
	err = a.projects.Insert(ctx, project)
	if err != nil {
		// 插库失败 → 退积分（手动补偿"回滚"），钱不能丢
		_ = a.points.Refund(ctx, orderID, req.UserID, req.ProjectType)
		return nil, err
	}

	// 5. 成功：返回扣分后的余额
	return contracts.CreateSuccessResp(cost, balance-cost), nil
}

// marshalProjectInput 序列化 project_input 为 JSON 字符串（存入 cg_creation_project.project_input）。
func marshalProjectInput(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	b, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return string(b)
}
