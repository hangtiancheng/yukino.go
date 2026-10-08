package app

import (
	"go.uber.org/dig"

	"github.com/hangtiancheng/yukino.go/components/timer/common/conf"
	"github.com/hangtiancheng/yukino.go/components/timer/internal/app/migrator"
	"github.com/hangtiancheng/yukino.go/components/timer/internal/app/monitor"
	"github.com/hangtiancheng/yukino.go/components/timer/internal/app/scheduler"
	"github.com/hangtiancheng/yukino.go/components/timer/internal/app/webserver"
	task_dao "github.com/hangtiancheng/yukino.go/components/timer/internal/dao/task"
	timer_dao "github.com/hangtiancheng/yukino.go/components/timer/internal/dao/timer"
	executor_service "github.com/hangtiancheng/yukino.go/components/timer/internal/service/executor"
	migrator_service "github.com/hangtiancheng/yukino.go/components/timer/internal/service/migrator"
	monitor_service "github.com/hangtiancheng/yukino.go/components/timer/internal/service/monitor"
	scheduler_service "github.com/hangtiancheng/yukino.go/components/timer/internal/service/scheduler"
	triggerservice "github.com/hangtiancheng/yukino.go/components/timer/internal/service/trigger"
	web_service "github.com/hangtiancheng/yukino.go/components/timer/internal/service/webserver"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/bloom"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/cron"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/hash"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/mysql"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/promethus"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/redis"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/xhttp"
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
	c.Provide(hash.NewMurmur3AltEncryptor)
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
