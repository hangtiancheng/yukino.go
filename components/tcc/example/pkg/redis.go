package pkg

import (
	"fmt"
	"sync"

	"github.com/hangtiancheng/yukino.go/components/distributed_lock"
)

const (
	network  = "tcp"
	address  = ""
	password = ""
)

var (
	redisClient *distributed_lock.Client
	once        sync.Once
)

func NewRedisClient(network, address, password string) *distributed_lock.Client {
	return distributed_lock.NewClient(network, address, password)
}

func GetRedisClient() *distributed_lock.Client {
	once.Do(func() {
		redisClient = distributed_lock.NewClient(network, address, password)
	})
	return redisClient
}

func BuildTXKey(componentID, txID string) string {
	return fmt.Sprintf("txKey:%s:%s", componentID, txID)
}

func BuildTXDetailKey(componentID, txID string) string {
	return fmt.Sprintf("txDetailKey:%s:%s", componentID, txID)
}

func BuildDataKey(componentID, txID, bizID string) string {
	return fmt.Sprintf("txKey:%s:%s:%s", componentID, txID, bizID)
}

func BuildTXLockKey(componentID, txID string) string {
	return fmt.Sprintf("txLockKey:%s:%s", componentID, txID)
}

func BuildTXRecordLockKey() string {
	return "tcc:txRecord:lock"
}
