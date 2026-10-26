package domain

import "context"

// BlacklistChecker 风控治理黑名单判定接口（应用层依赖注入，实现位于 infrastructure）。
//
// 设计意图：黑名单以 Redis 实现（risk:blacklist:{userID}:{subProduct}），但应用层保持
// 纯依赖倒置——不直接持有 redis.Client、不 import infrastructure；由基础设施层实现本接口
// 并注入。nil 注入时 RiskAuditApp 视为"未启用黑名单检查"，直接放行。
type BlacklistChecker interface {
	// IsBlacklisted 查询 userID 在 subProduct 下是否命中黑名单。
	IsBlacklisted(ctx context.Context, userID int64, subProduct string) bool
}

// BlacklistWriter 风控治理黑名单写入接口（评估 job 依赖注入，实现位于 infrastructure）。
//
// 与 BlacklistChecker 对称：评估 job 通过接口写黑名单，不直接持有 redis.Client。
// nil 注入时 RiskOfflineJob 视为"未启用黑名单写入"，跳过写黑名单（评估不中断）。
type BlacklistWriter interface {
	// SetBlacklisted 写入 userID 在 subProduct 下的黑名单。
	SetBlacklisted(ctx context.Context, userID int64, subProduct string) error
}
