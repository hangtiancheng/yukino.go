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

package app

import (
	"go.uber.org/dig"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/app/migrator"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/app/monitor"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/app/scheduler"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/app/webserver"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/conf"
	task_dao "github.com/hangtiancheng/yukino.go/apps/timer_demo/dao/task"
	timer_dao "github.com/hangtiancheng/yukino.go/apps/timer_demo/dao/timer"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/bloom"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/cron"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/hash"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/mysql"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/promethus"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/redis"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/xhttp"
	executor_service "github.com/hangtiancheng/yukino.go/apps/timer_demo/service/executor"
	migrator_service "github.com/hangtiancheng/yukino.go/apps/timer_demo/service/migrator"
	monitor_service "github.com/hangtiancheng/yukino.go/apps/timer_demo/service/monitor"
	scheduler_service "github.com/hangtiancheng/yukino.go/apps/timer_demo/service/scheduler"
	triggerservice "github.com/hangtiancheng/yukino.go/apps/timer_demo/service/trigger"
	web_service "github.com/hangtiancheng/yukino.go/apps/timer_demo/service/webserver"
)

var (
	container *dig.Container
)

func init() {
	container = dig.New()

	provideConfig(container)
	providePKG(container)
	provideDAO(container)
	provideService(container)
	provideApp(container)
}

func provideConfig(c *dig.Container) {
	c.Provide(conf.DefaultMysqlConfProvider)
	c.Provide(conf.DefaultSchedulerAppConfProvider)
	c.Provide(conf.DefaultTriggerAppConfProvider)
	c.Provide(conf.DefaultWebServerAppConfProvider)
	c.Provide(conf.DefaultRedisConfigProvider)
	c.Provide(conf.DefaultMigratorAppConfProvider)
}

func providePKG(c *dig.Container) {
	c.Provide(bloom.NewFilter)
	c.Provide(hash.NewMurmur3Encryptor)
	c.Provide(hash.NewSHA1Encryptor)
	c.Provide(redis.GetClient)
	c.Provide(mysql.GetClient)
	c.Provide(cron.NewCronParser)
	c.Provide(xhttp.NewJSONClient)
	c.Provide(promethus.GetReporter)
}

func provideDAO(c *dig.Container) {
	c.Provide(timer_dao.NewTimerDAO)
	c.Provide(task_dao.NewTaskDAO)
	c.Provide(task_dao.NewTaskCache)
}

func provideService(c *dig.Container) {
	c.Provide(migrator_service.NewWorker)
	c.Provide(migrator_service.NewWorker)
	c.Provide(web_service.NewTaskService)
	c.Provide(web_service.NewTimerService)
	c.Provide(executor_service.NewTimerService)
	c.Provide(executor_service.NewWorker)
	c.Provide(triggerservice.NewWorker)
	c.Provide(triggerservice.NewTaskService)
	c.Provide(scheduler_service.NewWorker)
	c.Provide(monitor_service.NewWorker)
}

func provideApp(c *dig.Container) {
	c.Provide(migrator.NewMigratorApp)
	c.Provide(webserver.NewTaskApp)
	c.Provide(webserver.NewTimerApp)
	c.Provide(webserver.NewServer)
	c.Provide(scheduler.NewWorkerApp)
	c.Provide(monitor.NewMonitorApp)
}

func GetSchedulerApp() *scheduler.WorkerApp {
	var schedulerApp *scheduler.WorkerApp
	if err := container.Invoke(func(_s *scheduler.WorkerApp) {
		schedulerApp = _s
	}); err != nil {
		panic(err)
	}
	return schedulerApp
}

func GetWebServer() *webserver.Server {
	var server *webserver.Server
	if err := container.Invoke(func(_s *webserver.Server) {
		server = _s
	}); err != nil {
		panic(err)
	}
	return server
}

func GetMigratorApp() *migrator.MigratorApp {
	var migratorApp *migrator.MigratorApp
	if err := container.Invoke(func(_m *migrator.MigratorApp) {
		migratorApp = _m
	}); err != nil {
		panic(err)
	}
	return migratorApp
}

func GetMonitorApp() *monitor.MonitorApp {
	var monitorApp *monitor.MonitorApp
	if err := container.Invoke(func(_m *monitor.MonitorApp) {
		monitorApp = _m
	}); err != nil {
		panic(err)
	}
	return monitorApp
}
