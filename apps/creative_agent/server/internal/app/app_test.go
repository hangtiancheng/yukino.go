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
			Port:     "1", // privileged port; nothing listens on it locally, so the connection is refused immediately
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
	// When MySQL is unreachable, gorm surfaces the underlying dial error.
	testx.Contains(t, err.Error(), "dial tcp")
}
