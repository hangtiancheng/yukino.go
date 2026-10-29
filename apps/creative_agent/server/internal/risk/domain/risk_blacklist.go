package domain

import "context"

// BlacklistChecker is the risk-governance blacklist check interface (application-layer dependency injection; the implementation lives in infrastructure).
//
// Design intent: the blacklist is Redis-backed (risk:blacklist:{userID}:{subProduct}), but the application layer keeps
// pure dependency inversion — it holds no redis.Client and imports no infrastructure; the infrastructure layer implements this interface
// and injects it. With nil injection, RiskAuditApp treats it as "blacklist checks disabled" and passes directly.
type BlacklistChecker interface {
	// IsBlacklisted checks whether userID hits the blacklist under subProduct.
	IsBlacklisted(ctx context.Context, userID int64, subProduct string) bool
}

// BlacklistWriter is the risk-governance blacklist write interface (evaluation-job dependency injection; the implementation lives in infrastructure).
//
// Symmetric with BlacklistChecker: the evaluation job writes the blacklist through the interface, holding no redis.Client directly.
// With nil injection, RiskOfflineJob treats it as "blacklist writes disabled" and skips writing (evaluation is not interrupted).
type BlacklistWriter interface {
	// SetBlacklisted writes the blacklist entry of userID under subProduct.
	SetBlacklisted(ctx context.Context, userID int64, subProduct string) error
}
