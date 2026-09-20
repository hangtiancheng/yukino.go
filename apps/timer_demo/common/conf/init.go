// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package conf

import (
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
		panic(err)
	}

	data, err := os.ReadFile(path + "/conf.yml")
	if err != nil {
		panic(err)
	}

	if err := yaml.Unmarshal(data, &gConf); err != nil {
		panic(err)
	}

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
