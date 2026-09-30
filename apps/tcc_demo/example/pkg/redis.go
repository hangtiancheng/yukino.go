package pkg

import (
	"fmt"
	"sync"

	"github.com/hangtiancheng/yukino.go/apps/redis_lock"
)

const (
	network  = "tcp"
	address  = ""
	password = ""
)

var (
	redisClient *redis_lock.Client
	once        sync.Once
)

func NewRedisClient(network, address, password string) *redis_lock.Client {
	return redis_lock.NewClient(network, address, password)
}

func GetRedisClient() *redis_lock.Client {
	once.Do(func() {
		redisClient = redis_lock.NewClient(network, address, password)
	})
	return redisClient
}

// BuildTXKey constructs the transaction ID key for idempotency deduplication
func BuildTXKey(componentID, txID string) string {
	return fmt.Sprintf("txKey:%s:%s", componentID, txID)
}

func BuildTXDetailKey(componentID, txID string) string {
	return fmt.Sprintf("txDetailKey:%s:%s", componentID, txID)
}

// BuildDataKey constructs the request ID key for state machine tracking
func BuildDataKey(componentID, txID, bizID string) string {
	return fmt.Sprintf("txKey:%s:%s:%s", componentID, txID, bizID)
}

// BuildTXLockKey constructs the transaction lock key
func BuildTXLockKey(componentID, txID string) string {
	return fmt.Sprintf("txLockKey:%s:%s", componentID, txID)
}

func BuildTXRecordLockKey() string {
	return "tcc_demo:txRecord:lock"
}
