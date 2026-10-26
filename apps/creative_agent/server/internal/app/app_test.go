package app_test

import (
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/config"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/app"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

func TestNewRequiresMySQLConnection(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Port: "18080"},
		JWT:    config.JWTConfig{Secret: "test-secret"},
		Log:    config.LogConfig{Level: "error"},
		MySQL: config.MySQLConfig{
			Host:     "127.0.0.1",
			Port:     "1", // 特权端口，本机不会有 MySQL 监听，连接立即被拒绝
			User:     "test",
			Password: "",
			Database: "test_db",
		},
		Redis: config.RedisConfig{
			Host: "127.0.0.1",
			Port: "6379",
		},
	}

	_, err := app.New(cfg)
	testx.Error(t, err)
	// MySQL 不可达时，gorm 透出底层 dial 错误。
	testx.Contains(t, err.Error(), "dial tcp")
}
