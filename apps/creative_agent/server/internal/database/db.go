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

// Init initializes the MySQL connection and runs AutoMigrate for all models.
// logger is used for zap JSON output of GORM slow SQL; the SQL tracing plugin uses the global provider (set by otelx.Init).
//
// The schema has two parts:
//   - agent domain (6 cg_-prefixed tables + users): GORM models in internal/model
//   - base-server bounded contexts: triggered by each infrastructure layer's Migrate
//     (gorm tags live only in the infrastructure layer; database does not care about concrete schemas)
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

	// Create one span per SQL statement (defaults to the global TracerProvider set by otelx.Init).
	// A minimal in-house plugin replacing gorm.io/plugin/opentelemetry (zero extra dependencies).
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

	// Tables of the base-server bounded contexts (the users table columns are aligned with model.User; idempotent).
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
