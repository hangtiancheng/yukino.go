package service

import (
	"context"
	"fmt"
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
	if ctx == nil || w.appConfigProvider == nil || w.appConfigProvider.Get() == nil || w.lockService == nil {
		return fmt.Errorf("migrator requires a context, configuration, and lock service")
	}
	conf := w.appConfigProvider.Get()
	interval := time.Duration(conf.MigrateStepMinutes) * time.Minute
	if interval <= 0 {
		return fmt.Errorf("migrator step interval must be positive")
	}
	if conf.MigrateTryLockMinutes <= 0 || conf.MigrateSuccessExpireMinutes <= 0 {
		return fmt.Errorf("migrator lock expirations must be positive")
	}
	ticker := time.NewTicker(interval)
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

		keepLock := false
		deferUnlock := func() {
			if keepLock {
				return
			}
			unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second*2)
			defer cancel()
			if err := locker.Unlock(unlockCtx); err != nil {
				log.ErrorContextf(ctx, "migrator unlock failed, err: %v", err)
			}
		}
		if err := w.migrate(ctx); err != nil {
			log.ErrorContextf(ctx, "migrate failed, err: %v", err)
			deferUnlock()
			continue
		}

		if err := locker.ExpireLock(ctx, int64(conf.MigrateSuccessExpireMinutes)*int64(time.Minute/time.Second)); err != nil {
			log.ErrorContextf(ctx, "migrator set success lock expiration failed, err: %v", err)
			deferUnlock()
			continue
		}
		keepLock = true
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
	for i, timer := range timers {
		nexts, err := w.cronParser.NextsBetween(timer.Cron, start, end)
		if err != nil {
			return fmt.Errorf("parse timer %d cron: %w", timer.ID, err)
		}
		if err := w.timerDAO.BatchCreateRecords(ctx, timer.BatchTasksFromTimer(nexts)); err != nil {
			return fmt.Errorf("create records for timer %d: %w", timer.ID, err)
		}
		if i < len(timers)-1 {
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
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
