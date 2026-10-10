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

var gConf GlobalConf = GlobalConf{
	Migrator: &MigratorAppConf{
		WorkersNum:                  1000,
		MigrateStepMinutes:          60,
		MigrateSuccessExpireMinutes: 120,
		MigrateTryLockMinutes:       20,
		TimerDetailCacheMinutes:     2,
	},

	Scheduler: &SchedulerAppConf{
		WorkersNum:             100,
		BucketsNum:             10,
		TryLockSeconds:         70,
		TryLockGapMilliSeconds: 100,
		SuccessExpireSeconds:   130,
	},

	Trigger: &TriggerAppConf{
		ZRangeGapSeconds: 1,
		WorkersNum:       10000,
	},

	WebServer: &WebServerAppConf{
		Port: 8092,
	},
	Redis: &RedisConfig{
		Network:            "tcp",
		MaxIdle:            2000,
		IdleTimeoutSeconds: 30,
		MaxActive:          1000,
		Wait:               true,
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
