package monitor

import (
	"context"
	"encoding/json"
	"fmt"

	creationdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// MaxCompensationRetry 补偿重试上限：超过则放弃（可能需退积分兜底，见 §5.3）。
const MaxCompensationRetry = 3

// RunCompensationJob 补偿重试 job：扫描 status=0 的补偿记录，按 BizType 分类重试。
//
// 重试语义（对齐 §5.3）：
//   - 成功 → MarkDone（status=1）
//   - 失败 → IncrRetryCount；若 RetryCount >= MaxCompensationRetry → 放弃（MarkDone）
//
// 返回错误仅表示扫描本身失败（DB 查询异常）；单条记录重试失败不回滚整个 job。
func RunCompensationJob(ctx context.Context, deps Deps) error {
	if deps.Compensations == nil {
		return nil // 未注入补偿仓储（装配缺省场景），跳过
	}
	pending, err := deps.Compensations.GetPending(ctx, 100)
	if err != nil {
		return err
	}
	for _, comp := range pending {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		retryErr := retryCompensation(ctx, deps, comp.BizType, comp.Payload)
		if retryErr == nil {
			_ = deps.Compensations.MarkDone(ctx, comp.ID)
			continue
		}
		// 重试失败：计数 +1，超上限则放弃
		_ = deps.Compensations.IncrRetryCount(ctx, comp.ID)
		if comp.RetryCount+1 >= MaxCompensationRetry {
			// 放弃前兜底：async_audit 类退积分 + 置 fail（钱不悬空）
			abandonCompensation(ctx, deps, comp.BizType, comp.Payload)
			_ = deps.Compensations.MarkDone(ctx, comp.ID)
		}
	}
	return nil
}

// retryCompensation 按 BizType 分发重试。
//
//   - BizTypeAsyncAudit(1) → riskAuditApp.RetryAsyncAudit（复用原始 auditing 记录重新提交）
//   - BizTypeCallbackAgent(2) → dispatcher.RetryCallbackAgent（重构 extend_params 同步重放）
func retryCompensation(ctx context.Context, deps Deps, bizType int, payload string) error {
	var p map[string]any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return fmt.Errorf("解析补偿 payload 失败: %w", err)
	}
	switch bizType {
	case creationdomain.BizTypeAsyncAudit:
		if deps.RiskAudit == nil {
			return fmt.Errorf("risk audit app 未注入")
		}
		return deps.RiskAudit.RetryAsyncAudit(ctx, p)
	case creationdomain.BizTypeCallbackAgent:
		if deps.Dispatcher == nil {
			return fmt.Errorf("callback dispatcher 未注入")
		}
		return deps.Dispatcher.RetryCallbackAgent(ctx, p)
	default:
		return fmt.Errorf("未知补偿类型: %d", bizType)
	}
}

// abandonCompensation 补偿重试被放弃时的兜底：async_audit 类退积分 + 置 fail。
//
// 对齐 §5.3「补偿被放弃时的兜底」：异步风控提交彻底失败时，作品可能一直停在
// running/audit 占用积分。用 payload 里的 third_id 查作品，若仍可处理则退积分 + 置 fail，
// 保证用户的钱能回来。
func abandonCompensation(ctx context.Context, deps Deps, bizType int, payload string) {
	if bizType != creationdomain.BizTypeAsyncAudit {
		return // callback_agent 只是推卡失败，不影响钱，交给超时 job 兜底
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return
	}
	thirdID, _ := p["third_id"].(string)
	if thirdID == "" {
		return
	}
	project, err := deps.Projects.GetByProjectID(ctx, thirdID)
	if err != nil {
		return
	}
	if deps.PostCreate == nil {
		return
	}
	_ = deps.PostCreate.RefundAndFailTimeout(ctx, project)
}
