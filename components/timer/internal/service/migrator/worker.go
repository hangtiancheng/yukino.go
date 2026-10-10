package service

import (
	"context"
	"time"

	common_conf "github.com/hangtiancheng/yukino.go/components/timer/common/conf"
	"github.com/hangtiancheng/yukino.go/components/timer/common/consts"
	"github.com/hangtiancheng/yukino.go/components/timer/common/utils"
	task_dao "github.com/hangtiancheng/yukino.go/components/timer/internal/dao/task"
	timer_dao "github.com/hangtiancheng/yukino.go/components/timer/internal/dao/timer"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/cron"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/log"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/pool"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/redis"
)

type Worker struct {
	timerDAO          *timer_dao.TimerDAO
	taskDAO           *task_dao.TaskDAO
	taskCache         *task_dao.TaskCache
	cronParser        *cron.CronParser
	lockService       *redis.Client
	appConfigProvider *common_conf.MigratorAppConfProvider
	pool              pool.WorkerPool
}

func NewWorker(timerDAO *timer_dao.TimerDAO, taskDAO *task_dao.TaskDAO, taskCache *task_dao.TaskCache, lockService *redis.Client,
	cronParser *cron.CronParser, appConfigProvider *common_conf.MigratorAppConfProvider) *Worker {
	return &Worker{
		pool:              pool.NewGoWorkerPool(appConfigProvider.Get().WorkersNum),
		timerDAO:          timerDAO,
		taskDAO:           taskDAO,
		taskCache:         taskCache,
		lockService:       lockService,
		cronParser:        cronParser,
		appConfigProvider: appConfigProvider,
	}
}

func (w *Worker) Start(ctx context.Context) error {
	conf := w.appConfigProvider.Get()
	ticker := time.NewTicker(time.Duration(conf.MigrateStepMinutes) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		log.InfoContext(ctx, "migrator ticking...")

		locker := w.lockService.GetDistributionLock(utils.GetMigratorLockKey(utils.GetStartHour(time.Now())))
		if err := locker.Lock(ctx, int64(conf.MigrateTryLockMinutes)*int64(time.Minute/time.Second)); err != nil {
			log.ErrorContextf(ctx, "migrator get lock failed, key: %s, err: %v", utils.GetMigratorLockKey(utils.GetStartHour(time.Now())), err)
			continue
		}

		if err := w.migrate(ctx); err != nil {
			log.ErrorContextf(ctx, "migrate failed, err: %v", err)
			continue
		}

		_ = locker.ExpireLock(ctx, int64(conf.MigrateSuccessExpireMinutes)*int64(time.Minute/time.Second))
	}
}

func (w *Worker) migrate(ctx context.Context) error {
	timers, err := w.timerDAO.GetTimers(ctx, timer_dao.WithStatus(int32(consts.Enable.ToInt())))
	if err != nil {
		return err
	}

	conf := w.appConfigProvider.Get()
	now := time.Now()
	start, end := utils.GetStartHour(now.Add(time.Duration(conf.MigrateStepMinutes)*time.Minute)), utils.GetStartHour(now.Add(2*time.Duration(conf.MigrateStepMinutes)*time.Minute))
	for _, timer := range timers {
		nexts, _ := w.cronParser.NextsBetween(timer.Cron, start, end)
		if err := w.timerDAO.BatchCreateRecords(ctx, timer.BatchTasksFromTimer(nexts)); err != nil {
			log.ErrorContextf(ctx, "migrator batch create records for timer: %d failed, err: %v", timer.ID, err)
		}
		time.Sleep(5 * time.Second)
	}

	return w.migrateToCache(ctx, start, end)
}

func (w *Worker) migrateToCache(ctx context.Context, start, end time.Time) error {
	tasks, err := w.taskDAO.GetTasks(ctx, task_dao.WithStartTime(start), task_dao.WithEndTime(end))
	if err != nil {
		log.ErrorContextf(ctx, "migrator batch get tasks failed, err: %v", err)
		return err
	}
	return w.taskCache.BatchCreateTasks(ctx, tasks, start, end)
}
