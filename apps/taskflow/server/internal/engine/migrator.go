package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/cronx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/obsx"
	"github.com/hangtiancheng/yukino.go/components/redis_lock"
	"github.com/hangtiancheng/yukino.go/components/time_wheel"
)

const (
	migratorLockKey     = "taskflow:lock:migrator"
	SingletonMigrator   = "taskflow:singleton:migrator"
	SingletonMonitor    = "taskflow:singleton:monitor"
	defaultTaskTimezone = "Asia/Shanghai"
)

type FireCallback struct {
	ExecutionID uint   `json:"execution_id"`
	FireKey     string `json:"fire_key"`
}

type Migrator struct {
	cfg      *conf.Config
	dao      *dao.DAO
	wheel    *time_wheel.RTimeWheel
	registry *SingletonRegistry
	locker   *redis_lock.Client
	defs     *DefinitionCache

	stopOnce sync.Once
	stopChan chan struct{}
	doneChan chan struct{}
}

func NewMigrator(cfg *conf.Config, d *dao.DAO, wheel *time_wheel.RTimeWheel, registry *SingletonRegistry, locker *redis_lock.Client, defs *DefinitionCache) *Migrator {
	return &Migrator{
		cfg:      cfg,
		dao:      d,
		wheel:    wheel,
		registry: registry,
		locker:   locker,
		defs:     defs,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

func (m *Migrator) Start() {
	go m.run()
}

func (m *Migrator) Stop() {
	m.stopOnce.Do(func() { close(m.stopChan) })
	<-m.doneChan
}

func (m *Migrator) run() {
	defer close(m.doneChan)
	ticker := time.NewTicker(time.Duration(m.cfg.Scheduler.MigrateTickSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.tick()
		}
	}
}

func (m *Migrator) tick() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("migrator tick panicked", "panic", r)
			obsx.CapturePanic(context.Background(), r, map[string]string{"role": "migrator"})
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.cfg.Scheduler.MigrateTickSeconds)*time.Second)
	defer cancel()

	if !m.registry.IsOwner(ctx, SingletonMigrator) {
		return
	}

	lock := redis_lock.NewRedisLock(migratorLockKey, m.locker,
		redis_lock.WithExpireSeconds(int64(m.cfg.Scheduler.MigrateTickSeconds*2)))
	if err := lock.Lock(ctx); err != nil {
		return
	}
	defer func() { _ = lock.Unlock(ctx) }()

	m.materialize(ctx)
}

func (m *Migrator) materialize(ctx context.Context) {
	tasks, err := m.dao.EnabledScheduledTasks(ctx)
	if err != nil {
		slog.Error("load enabled scheduled tasks", "err", err)
		return
	}

	now := time.Now()
	step := time.Duration(m.cfg.Scheduler.MigrateStepMinutes) * time.Minute
	windowEnd := now.Add(step)
	recoverStart := now.Add(-time.Duration(m.cfg.Scheduler.OverdueRecoverMinutes) * time.Minute)
	callbackURL := m.cfg.Server.BaseURL() + "/internal/v1/fire"

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			return
		default:
		}
		m.materializeTask(ctx, task, now, recoverStart, windowEnd, callbackURL)
	}
}

func (m *Migrator) materializeTask(ctx context.Context, task *po.ScheduledTask, now, recoverStart, windowEnd time.Time, callbackURL string) {
	loc, err := time.LoadLocation(task.Timezone)
	if err != nil || task.Timezone == "" {
		loc, err = time.LoadLocation(defaultTaskTimezone)
		if err != nil {
			loc = time.Local
		}
	}

	schedule, err := cronx.Parse(task.CronExpr)
	if err != nil {
		slog.Error("invalid cron expr, skipping task", "task_id", task.ID, "cron", task.CronExpr, "err", err)
		obsx.CaptureError(ctx, fmt.Errorf("invalid cron expr %q on scheduled task %d: %w", task.CronExpr, task.ID, err), map[string]string{
			"task_id": fmt.Sprintf("%d", task.ID),
		})
		return
	}

	if task.CreatedAt.After(recoverStart) {
		recoverStart = task.CreatedAt
	}
	cursor := recoverStart.In(loc)
	for {
		fireAt := schedule.Next(cursor)
		if fireAt.IsZero() || fireAt.After(windowEnd) {
			break
		}
		cursor = fireAt

		if err := m.registerFire(ctx, task, fireAt, callbackURL, now); err != nil {
			slog.Error("register fire", "task_id", task.ID, "fire_at", fireAt, "err", err)
		}
	}

	if next := schedule.Next(now.In(loc)); !next.IsZero() {
		nextUTC := next.UTC()
		if task.NextFireAt == nil || !task.NextFireAt.Equal(nextUTC) {
			task.NextFireAt = &nextUTC
			if err := m.dao.DB().WithContext(ctx).Model(&po.ScheduledTask{}).Where("id = ? AND cron_expr = ? AND timezone = ?", task.ID, task.CronExpr, task.Timezone).Update("next_fire_at", nextUTC).Error; err != nil {
				slog.Warn("persist next_fire_at", "task_id", task.ID, "err", err)
			}
			m.defs.InvalidateScheduledTask(ctx, task.ID)
		}
	}
}

func (m *Migrator) registerFire(ctx context.Context, task *po.ScheduledTask, fireAt time.Time, callbackURL string, now time.Time) error {
	fireKey := SchedFireKey(task.ID, fireAt)

	triggerInfo, _ := json.Marshal(map[string]any{
		"cron":         task.CronExpr,
		"timezone":     task.Timezone,
		"scheduled_at": fireAt.Format(time.RFC3339),
	})

	candidate := &po.Execution{
		TaskType:       po.TaskTypeScheduled,
		TaskID:         task.ID,
		TaskName:       task.Name,
		FireKey:        fireKey,
		Status:         po.StatusPending,
		FireAt:         fireAt,
		TriggerInfo:    string(triggerInfo),
		PromptSnapshot: task.Prompt,
		ModelSnapshot:  task.Model,
	}
	_, id, err := m.dao.InsertPending(ctx, candidate)
	if err != nil {
		return fmt.Errorf("insert pending execution: %w", err)
	}

	exec, err := m.dao.GetExecution(ctx, id)
	if err != nil {
		return err
	}
	if exec.Status != po.StatusPending {
		return nil
	}

	executeAt := fireAt
	if !executeAt.After(now.Add(time.Second)) {
		executeAt = now.Add(5 * time.Second)
	}

	callback := FireCallback{ExecutionID: id, FireKey: fireKey}
	return m.wheel.AddTask(ctx, fireKey, &time_wheel.RTaskElement{
		CallbackURL: callbackURL,
		Method:      http.MethodPost,
		Req:         callback,
		Header: map[string]string{
			"Authorization":    "Bearer " + m.cfg.Server.InternalToken(),
			"Content-Type":     "application/json",
			"X-Taskflow-Node":  m.cfg.Node.ID,
			"X-Taskflow-Fired": fireAt.Format(time.RFC3339),
		},
	}, executeAt)
}

func SchedFireKey(taskID uint, fireAt time.Time) string {
	return fmt.Sprintf("sched:%d:%d", taskID, fireAt.Unix())
}
