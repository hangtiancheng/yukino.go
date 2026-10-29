//go:build integration

package app_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Connectivity probe: verifies that remote MySQL / Redis are reachable and credentials are correct.
//
// Usage: make ping   (or go test -tags=integration ./internal/app/ -run TestConnectivity -v)
//
// Reuses the project's real connection parameters (config/config.json + config/secrets.json),
// but does not run AutoMigrate — table checks against the remote DB trigger many Information_schema queries
// taking ~15s, while connectivity only needs to verify the connection and credentials. database.Init handles table creation at service startup.
func TestConnectivity(t *testing.T) {
	// go test's CWD is internal/app/, while config.Load merges secrets.json using relative paths;
	// switch to the repository root first, consistent with internal/app/integration_test.go.
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	testx.NoError(t, os.Chdir(repoRoot))

	cfg := config.Load("config/config.json")

	// To avoid probing the remote DB0 cache, override with REDIS_DB (e.g. REDIS_DB=15).
	if v := os.Getenv("REDIS_DB"); v != "" {
		var db int
		fmt.Sscanf(v, "%d", &db)
		cfg.Redis.DB = db
	}

	t.Run("mysql", func(t *testing.T) {
		start := time.Now()
		db, err := gorm.Open(mysql.Open(cfg.MySQL.DSN()), &gorm.Config{})
		if err != nil {
			t.Fatalf("failed to connect to remote MySQL (address=%s:%s user=%s db=%s): %v",
				cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.User, cfg.MySQL.Database, err)
		}
		sqlDB, err := db.DB()
		testx.NoError(t, err)
		defer sqlDB.Close()

		// Real authentication + one simple query, confirming the whole chain works.
		var one int
		testx.NoError(t, db.Raw("SELECT 1").Scan(&one).Error)
		testx.Equal(t, 1, one)
		t.Logf("MySQL OK: %s:%s/%s (auth + SELECT 1, took %s)",
			cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.Database, time.Since(start).Round(time.Millisecond))
	})

	t.Run("redis", func(t *testing.T) {
		start := time.Now()
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr(),
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		defer rdb.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		testx.NoError(t, rdb.Ping(ctx).Err(), "failed to connect to remote Redis (addr=%s db=%d)", cfg.Redis.Addr(), cfg.Redis.DB)
		t.Logf("Redis OK: %s db=%d (Ping, took %s)", cfg.Redis.Addr(), cfg.Redis.DB, time.Since(start).Round(time.Millisecond))
	})
}
