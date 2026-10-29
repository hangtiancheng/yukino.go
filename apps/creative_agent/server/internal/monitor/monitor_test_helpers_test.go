//go:build integration

// Package monitor integration-test shared assembly: builds the new-architecture (DDD) monitor Deps against testdb.
//
// Aligned with internal/app/app.go's assembly line but connecting to testdb (shared by monitor tests);
// returns Deps + the repositories (for tests to seed data/assert).
package monitor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	creationinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/dao"
	creationdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	creationapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/service"
	pointsapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/application"
	pointsinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/points/infrastructure"
	riskinfra "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	riskdomain "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	riskapp "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// monitorDepsBuild builds the new-architecture monitor dependencies with testdb + redis.
// Returns Deps and the repositories (for tests to seed data/assert).
type monitorDeps struct {
	deps      Deps
	db        *gorm.DB
	rdb       *redis.Client
	projects  creationdomain.ProjectRepository
	records   creationdomain.CallbackRecordRepository
	comp      creationdomain.CompensationRepository
	points    pointsapp.PointsApp
	riskAudit riskdomain.AuditRecordRepository
	riskStats riskdomain.RiskStatsRepository
}

// openMySQLTestDB switches to the repository root to read config and opens the testdb MySQL connection.
func openMySQLTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	testx.NoError(t, os.Chdir(repoRoot))

	cfg := config.Load("config/config.json")
	cfg.MySQL.Database = "testdb"
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.MySQL.User, cfg.MySQL.Password, cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.Database)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	testx.NoError(t, err)
	return db
}

// openRedisTestDB opens the testdb Redis (separate db, avoiding pollution of the development cache).
func openRedisTestDB(t *testing.T) *redis.Client {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	testx.NoError(t, os.Chdir(repoRoot))

	cfg := config.Load("config/config.json")
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr(), Password: cfg.Redis.Password, DB: 3})
	testx.NoError(t, rdb.Ping(context.Background()).Err())
	return rdb
}

// buildMonitorDeps builds the new-architecture monitor dependencies (assembly line equivalent to internal/app/app.go).
func buildMonitorDeps(t *testing.T, db *gorm.DB, rdb *redis.Client) *monitorDeps {
	t.Helper()

	// points
	pointsRepo := pointsinfra.NewPointsRepositoryImpl(db)
	pointsApp := pointsapp.NewPointsApp(pointsRepo)

	// risk
	riskRecords := riskinfra.NewAuditRecordRepositoryImpl(db)
	riskStatsRepo := riskinfra.NewRiskStatsRepositoryImpl(db)
	auditBackend := riskinfra.NewLocalAuditBackend()

	// creation
	projRepo := creationinfra.NewProjectRepositoryImpl(db)
	cbRepo := creationinfra.NewCallbackRecordRepositoryImpl(db)
	compRepo := creationinfra.NewCompensationRepositoryImpl(db)

	compSink := creationinfra.NewCompensationSinkImpl(compRepo)
	onAuditResult := creationinfra.NewOnAuditResultImpl(projRepo, pointsApp)
	blacklistChecker := riskinfra.NewRedisBlacklistChecker(rdb)
	riskAuditApp := riskapp.NewRiskAuditApp(auditBackend, riskRecords, compSink, blacklistChecker)
	auditOutputApp := riskapp.NewAuditOutputApp(riskRecords, onAuditResult)

	creationApp := creationapp.NewCreationApp(projRepo, cbRepo, riskAuditApp, pointsApp)
	dispatcher := creationinfra.NewCallbackDispatcher(
		creationinfra.NewCallbackDeps(projRepo, cbRepo, compRepo, riskAuditApp, pointsApp),
	)

	_ = auditOutputApp // the risk-callback loop is not directly exercised by monitor tests

	return &monitorDeps{
		deps: Deps{
			DB:            db,
			PostCreate:    creationApp.PostCreate,
			RiskAudit:     riskAuditApp,
			Dispatcher:    dispatcher,
			Projects:      projRepo,
			Compensations: compRepo,
			RiskStats:     riskStatsRepo,
			Redis:         rdb,
			Logger:        nil,
		},
		db:        db,
		rdb:       rdb,
		projects:  projRepo,
		records:   cbRepo,
		comp:      compRepo,
		points:    *pointsApp,
		riskAudit: riskRecords,
		riskStats: riskStatsRepo,
	}
}

// seedBalance gives the user an initial balance (flow_type=2 credit, equivalent to the old monitorSeedBalance).
func seedBalance(t *testing.T, db *gorm.DB, userID int64, amount int) {
	t.Helper()
	now := time.Now().Unix()
	testx.NoError(t, db.Exec(
		"INSERT INTO cg_points_flow (user_id, project_id, order_id, flow_type, amount, scene_desc, status, create_time) VALUES (?, 'seed', ?, 2, ?, 'test seed', 1, ?)",
		userID, fmt.Sprintf("monitor_seed_%d", userID), amount, now,
	).Error)
}

// truncateMonitorTables clears the tables involved in monitor tests (equivalent to the old truncateCreationTables).
func truncateMonitorTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	testx.NoError(t, db.Exec("TRUNCATE TABLE cg_creation_project").Error)
	testx.NoError(t, db.Exec("TRUNCATE TABLE cg_task_callback_record").Error)
	testx.NoError(t, db.Exec("TRUNCATE TABLE cg_points_flow").Error)
	testx.NoError(t, db.Exec("TRUNCATE TABLE cg_audit_compensation").Error)
	testx.NoError(t, db.Exec("TRUNCATE TABLE cg_risk_audit_record").Error)
}
