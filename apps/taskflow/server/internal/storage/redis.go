package storage

import (
	"os"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/redis/go-redis/v9"
)

func OpenRedis(cfg conf.RedisConf) redis.UniversalClient {
	addresses := cfg.Addresses
	if len(addresses) == 0 {
		addresses = []string{cfg.Address}
	}
	opts := &redis.UniversalOptions{Addrs: addresses, Password: cfg.Password, DB: cfg.DB, PoolSize: 64, MinIdleConns: 8, DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	switch cfg.Mode {
	case "sentinel":
		opts.MasterName = cfg.MasterName
		opts.SentinelPassword = os.Getenv(cfg.SentinelPasswordEnv)
	case "cluster":
		return redis.NewClusterClient(opts.Cluster())
	}
	return redis.NewUniversalClient(opts)
}
