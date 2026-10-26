package monitor

import (
	"context"
	"time"
)

// RunTimeoutJob 超时兜底 job：扫描超过 hours 小时仍处于 running 的项目，退积分 + 置 fail。
//
// 对齐 §5.4：卡在中间态的作品（算子失联、进程崩溃）可能永久停在 running。
// 定时扫描强制收尾，防止永久占用积分。这是最终兜底——所有补偿/重试都失效时，
// 靠它保证用户的钱能回来。
func RunTimeoutJob(ctx context.Context, deps Deps, hours int) error {
	if deps.Projects == nil || deps.PostCreate == nil {
		return nil // 未注入依赖（装配缺省场景），跳过
	}
	if hours <= 0 {
		hours = 24
	}
	deadline := time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
	stuck, err := deps.Projects.GetStuckProjects(ctx, deadline)
	if err != nil {
		return err
	}
	for _, p := range stuck {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		// RefundAndFailTimeout 内部幂等：仅 running 处理、仅 used 退积分
		_ = deps.PostCreate.RefundAndFailTimeout(ctx, p)
	}
	return nil
}
