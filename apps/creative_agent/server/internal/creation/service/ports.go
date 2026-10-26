package service

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// RiskPort 送审接口（risk.application 的 RiskAuditApp 实现，跨 context 端口）。
//
// creation 应用层不 import risk.infrastructure，只依赖本接口完成：
//   - 输入/产出风控（同步 SyncAudit、异步 AsyncAudit）
//   - 送审前黑名单检查（IsBlacklisted）
//   - 合并审计记录（NewSyncAuditRecorder，接口定义在 risk.domain，实现注入）
//
// 说明：原旧 Service 的 runPreCreateAudit 编排（文本组构建 + recorder 合并 + 黑名单检查）
// 留在 creation.application（属于创作工作流），底层送审/记录能力由 risk 端口提供。
type RiskPort interface {
	// SyncAudit 文本/图片同步风控：阻塞等待结果，立即返回。
	//   - passed=false + err=non-nil → 风控请求失败（系统错误，退积分分支）
	//   - passed=false + reason=non-empty → 内容不通过（命中风控，输入/产出风控卡）
	//   - passed=true → 通过
	SyncAudit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error)
	// AsyncAudit 视频异步风控：提交后立即返回，结果由 /inner/callback/risk 闭环。
	// 返回 audit_record ID（回调时透传对账用）。
	AsyncAudit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct, callbackURL string) (int64, error)
	// IsBlacklisted 送审前黑名单检查：命中返回 true（拦截）。
	IsBlacklisted(ctx context.Context, userID int64, subProduct string) bool
	// NewSyncAuditRecorder 创建一次同步风控的合并记录器（多次风控合并为一条审计记录）。
	NewSyncAuditRecorder(ctx context.Context, userID int64, projectID string, taskStepID int64, taskType string) domain.SyncAuditRecorder
}

// PointsPort 积分接口（points.application 的 PointsApp 实现，跨 context 端口）。
//
// creation 应用层不 import points.infrastructure，通过本接口完成积分扣退与余额查询。
type PointsPort interface {
	// GetBalance 查用户余额。
	GetBalance(ctx context.Context, userID int64) (int, error)
	// Deduct 扣积分（幂等：order_id UNIQUE 防重复扣）。
	// orderID 由调用方保证唯一（如 {projectID}_{retryNum}）。
	Deduct(ctx context.Context, userID int64, projectID, orderID, projectType string, amount int) (uint64, error)
	// Refund 退积分（幂等：已退过返回 nil 不报错）。
	Refund(ctx context.Context, orderID string, userID int64, projectType string) error
}
