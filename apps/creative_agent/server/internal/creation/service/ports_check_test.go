package service

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/application"
	riskapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
)

// Compile-time assertions: the risk/points application services directly implement the cross-domain ports defined by creation
// (dependency direction creation.app → risk.app / points.app; concrete instances are injected at assembly time).
var (
	_ RiskPort   = (*riskapp.RiskAuditApp)(nil)
	_ PointsPort = (*application.PointsApp)(nil)
)
