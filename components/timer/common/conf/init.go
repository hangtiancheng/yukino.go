package conf

import (
	"log"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

var (
	configOnce sync.Once
)

func init() {
	configOnce.Do(loadConfig)
}

// loadConfig reads ./conf.yml when present. Embedding applications may import
// timer packages (bloom filter, cron parser, worker pool) without
// shipping a timer conf.yml, so a missing or malformed file falls back to
// the built-in defaults instead of panicking.
func loadConfig() {
	path, err := os.Getwd()
	if err != nil {
		log.Printf("timer conf: getwd failed: %v; using built-in defaults", err)
		initProviders()
		return
	}

	data, err := os.ReadFile(path + "/conf.yml")
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("timer conf: read %s/conf.yml failed: %v; using built-in defaults", path, err)
		}
		initProviders()
		return
	}

	if err := yaml.Unmarshal(data, &gConf); err != nil {
		log.Printf("timer conf: parse %s/conf.yml failed: %v; using built-in defaults", path, err)
	}
	initProviders()
}

func initProviders() {
	defaultMigratorAppConfProvider = NewMigratorAppConfProvider(gConf.Migrator)
	defaultMysqlConfProvider = NewMysqlConfProvider(gConf.Mysql)
	defaultRedisConfProvider = NewRedisConfigProvider(gConf.Redis)
	defaultTriggerAppConfProvider = NewTriggerAppConfProvider(gConf.Trigger)
	defaultSchedulerAppConfProvider = NewSchedulerAppConfProvider(gConf.Scheduler)
	defaultWebServerAppConfProvider = NewWebServerAppConfProvider(gConf.WebServer)
}

// gConf holds the fallback default configuration.
var gConf GlobalConf = GlobalConf{
	Migrator: &MigratorAppConf{
		// Number of concurrent goroutines per node
		WorkersNum: 1000,
		// Time interval for each data migration step, in minutes
		MigrateStepMinutes: 60,
		// Lock expiration time updated after successful migration, in minutes
		MigrateSuccessExpireMinutes: 120,
		// Initial lock expiration time when the migrator acquires the lock, in minutes
		MigrateTryLockMinutes: 20,
		// How long the migrator caches timer details in memory ahead of time, in minutes
		TimerDetailCacheMinutes: 2,
	},

	Scheduler: &SchedulerAppConf{
		// Number of concurrent goroutines per node
		WorkersNum: 100,
		// Number of buckets
		BucketsNum: 10,
		// Initial lock expiration time when the scheduler acquires a distributed lock, in seconds
		TryLockSeconds: 70,
		// Interval between each lock acquisition attempt by the scheduler, in milliseconds
		TryLockGapMilliSeconds: 100,
		// Updated distributed lock duration after a time slice executes successfully, in seconds
		SuccessExpireSeconds: 130,
	},

	Trigger: &TriggerAppConf{
		// Interval at which the trigger polls the timer task zset, in seconds
		ZRangeGapSeconds: 1,
		// Number of concurrent goroutines
		WorkersNum: 10000,
	},

	WebServer: &WebServerAppConf{
		Port: 8092,
	},
	Redis: &RedisConfig{
		Network: "tcp",
		// Maximum number of idle connections
		MaxIdle: 2000,
		// Idle connection timeout, in seconds
		IdleTimeoutSeconds: 30,
		// Maximum number of active connections in the pool
		MaxActive: 1000,
		// Whether new requests wait or fail immediately when the pool is full
		Wait: true,
	},
	Mysql: &MySQLConfig{
		MaxOpenConns: 100,
		MaxIdleConns: 50,
	},
}

type GlobalConf struct {
	Migrator  *MigratorAppConf  `yaml:"migrator"`
	Mysql     *MySQLConfig      `yaml:"mysql"`
	Redis     *RedisConfig      `yaml:"redis"`
	Trigger   *TriggerAppConf   `yaml:"trigger"`
	Scheduler *SchedulerAppConf `yaml:"scheduler"`
	WebServer *WebServerAppConf `yaml:"webserver"`
}
