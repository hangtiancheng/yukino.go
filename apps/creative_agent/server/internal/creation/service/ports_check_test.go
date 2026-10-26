package service

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/application"
	riskapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
)

// 编译期断言：risk/points 的应用服务直接实现 creation 定义的跨域端口
// （依赖方向 creation.app → risk.app / points.app，装配时直接注入具体实例）。
var (
	_ RiskPort   = (*riskapp.RiskAuditApp)(nil)
	_ PointsPort = (*application.PointsApp)(nil)
)
