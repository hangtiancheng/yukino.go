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

// 连通性探测：验证远程 MySQL / Redis 可达且凭据正确。
//
// 用法：make ping   （或 go test -tags=integration ./internal/app/ -run TestConnectivity -v）
//
// 复用项目真实连接参数（config/config.json + config/secrets.json），
// 但不执行 AutoMigrate —— 建表检查对远程库会触发大量 Information_schema 查询
// 导致耗时 ~15s，而连通性只需验证连接与凭据。启动服务时由 database.Init 负责建表。
func TestConnectivity(t *testing.T) {
	// go test 的 CWD 是 internal/app/，而 config.Load 合并 secrets.json 用相对路径，
	// 先切到仓库根目录，与 internal/app/integration_test.go 保持一致。
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	testx.NoError(t, os.Chdir(repoRoot))

	cfg := config.Load("config/config.json")

	// 若不想探测到远程 DB0 缓存，可用 REDIS_DB 覆盖（例如 REDIS_DB=15）。
	if v := os.Getenv("REDIS_DB"); v != "" {
		var db int
		fmt.Sscanf(v, "%d", &db)
		cfg.Redis.DB = db
	}

	t.Run("mysql", func(t *testing.T) {
		start := time.Now()
		db, err := gorm.Open(mysql.Open(cfg.MySQL.DSN()), &gorm.Config{})
		if err != nil {
			t.Fatalf("连接远程 MySQL 失败（address=%s:%s user=%s db=%s）: %v",
				cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.User, cfg.MySQL.Database, err)
		}
		sqlDB, err := db.DB()
		testx.NoError(t, err)
		defer sqlDB.Close()

		// 真实认证 + 一次简单查询，确认整条链路可用。
		var one int
		testx.NoError(t, db.Raw("SELECT 1").Scan(&one).Error)
		testx.Equal(t, 1, one)
		t.Logf("MySQL OK：%s:%s/%s（认证 + SELECT 1，耗时 %s）",
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
		testx.NoError(t, rdb.Ping(ctx).Err(), "连接远程 Redis 失败（addr=%s db=%d）", cfg.Redis.Addr(), cfg.Redis.DB)
		t.Logf("Redis OK：%s db=%d（Ping，耗时 %s）", cfg.Redis.Addr(), cfg.Redis.DB, time.Since(start).Round(time.Millisecond))
	})
}
