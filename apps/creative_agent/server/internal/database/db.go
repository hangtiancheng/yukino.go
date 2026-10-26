package database

import (
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	authinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/auth/infrastructure"
	creationinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/dao"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	pointsinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/infrastructure"
	riskinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/otelx"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Init 初始化 MySQL 连接并对全部模型执行 AutoMigrate。
// logger 用于 GORM slow SQL 的 zap JSON 输出；SQL tracing 插件用全局 provider（由 otelx.Init 设置）。
//
// 表结构分两部分：
//   - agent 域（cg_ 前缀 6 张 + users）：internal/model 的 GORM 模型
//   - base-server 各 bounded context：由各自 infrastructure 层的 Migrate 触发
//     （gorm 标签只存在于基础设施层，database 不关心具体 schema）
func Init(cfg config.MySQLConfig, logger *zap.Logger) (*gorm.DB, error) {
	if logger == nil {
		logger = zap.L()
	}
	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		Logger: otelx.NewGormLogger(logger, 200*time.Millisecond),
	})
	if err != nil {
		return nil, err
	}

	// 每条 SQL 建一个 span（默认全局 TracerProvider，由 otelx.Init 设置）。
	// 自研极简插件，取代 gorm.io/plugin/opentelemetry（零额外依赖）。
	if err := db.Use(otelx.NewGormTracing()); err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(
		&model.User{},
		&model.CGSession{},
		&model.CGMessageRecord{},
		&model.CGAsyncRequest{},
		&model.CGTaskRun{},
		&model.CGTaskStep{},
		&model.CGCronTask{},
		&model.CGAgentThought{},
	); err != nil {
		return nil, err
	}

	// base-server 各 bounded context 的表（users 表与 model.User 列对齐，幂等）。
	if err := authinfra.Migrate(db); err != nil {
		return nil, err
	}
	if err := creationinfra.Migrate(db); err != nil {
		return nil, err
	}
	if err := pointsinfra.Migrate(db); err != nil {
		return nil, err
	}
	if err := riskinfra.Migrate(db); err != nil {
		return nil, err
	}

	return db, nil
}
