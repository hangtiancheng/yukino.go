package service

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// handleRetry 处理重试扣分（retry_type=1 失败重试 或 points_status != used）。
//
// 原项目语义：失败重试 = 重新生成一次 = 重新消耗一次积分。核心是 order_id 递增到
// retry_num+1（防重复扣分的对账依据），并把新订单号追加进 project.ext.order_ids。
//
// 流程：查余额 → 不足则积分不足卡 → 再扣一次 → 更新 project（retry_num+1、
// points_order_id、points_status=used、ext.order_ids 追加）→ 返回正常 pre_create 结果。
func (a *PreCreateApp) handleRetry(ctx context.Context, req *contracts.PreCreateReq, project *domain.Project) (*contracts.PreCreateResp, error) {
	memberCost := project.PointsCost
	if memberCost == 0 {
		memberCost = 1
	}

	// 1. 查余额
	currentScore, err := a.points.GetBalance(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if currentScore < memberCost {
		// 积分不足 → 返回积分不足卡（不扣分）
		return contracts.BuildInsufficientScoreResp(memberCost, currentScore), nil
	}

	// 2. 再扣一次，order_id = projectID_{retryNum+1}
	orderID := buildOrderID(project.ProjectID, project.RetryNum+1)
	_, err = a.points.Deduct(ctx, req.UserID, project.ProjectID, orderID, project.ProjectType, memberCost)
	if err != nil {
		return nil, err
	}

	// 3. 更新 project：retry_num+1、points_order_id=新orderID、points_status=used
	//    同时把新 orderID 追加进 project.ext.order_ids（对账用）
	project.RetryNum++
	newExt := domain.AppendOrderID(project.Ext, orderID)
	project.Ext = newExt // 同步内存值，后续 buildPreCreateSuccess 写 task_step 时保留 order_ids
	project.PointsOrderID = orderID
	project.PointsStatus = domain.PointStatusUsed
	_ = a.projects.Update(ctx, project)

	// 4. 解析输入并组装算子入参返回（重试成功后同样走正常 pre_create 流程）
	projectInput, err := contracts.ParseInput(req.ProjectInput)
	if err != nil {
		return contracts.FailCardResp(domain.CardTypeFailedRetry, costScore(req.ProjectType, nil)), nil
	}
	return a.buildPreCreateSuccess(ctx, req, project, projectInput)
}

// buildOrderID 生成积分流水的幂等键：{projectID}_{retryNum}。
func buildOrderID(projectID string, retryNum int) string {
	return fmt.Sprintf("%s_%d", projectID, retryNum)
}
