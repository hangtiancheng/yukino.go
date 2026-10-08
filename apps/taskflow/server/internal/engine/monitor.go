package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/obsx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/storage"
	"github.com/hangtiancheng/yukino.go/components/red_mq"
	"github.com/hangtiancheng/yukino.go/components/redis_lock"
	"github.com/hangtiancheng/yukino.go/components/time_wheel"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/pool"
	"github.com/redis/go-redis/v9"
)

const (
	monitorLockKey = "taskflow:lock:monitor"
	StatsRedisKey  = "taskflow:monitor:stats"
	// ExecMirrorKey is the redis hash mirroring hot execution statuses out of
	// MySQL (field = execution id, value = JSON {status, updated_at}). The
	// sync sweep keeps it aligned with the source of truth.
	ExecMirrorKey = "taskflow:exec:status"
	sweepKey      = "monitor-sweep"
	// execMirrorTTL bounds how long stale mirror entries may live.
	execMirrorTTL = 7 * 24 * time.Hour
)

// Monitor runs on every node but only the consistent-hash owner of
// SingletonMonitor performs the sweep: fail executions stuck in running,
// re-dispatch overdue pending executions, re-publish lost queued commands,
// synchronize MySQL data into redis (status mirror + definition-cache drift
// repair), and publish a stats snapshot for the dashboard API. The loop
// itself is driven by the in-process time wheel; per-row recovery work runs
// on a bounded timer worker pool.
type Monitor struct {
	cfg        *conf.Config
	dao        *dao.DAO
	dispatcher *Dispatcher
	defs       *DefinitionCache
	registry   *SingletonRegistry
	client     redis.UniversalClient
	locker     *redis_lock.Client
	producer   *red_mq.Producer
	pool       pool.WorkerPool

	wheel    *time_wheel.TimeWheel
	runMu    sync.Mutex
	cancel   context.CancelFunc
	ctx      context.Context
	stopOnce sync.Once
	stopChan chan struct{}
}

func NewMonitor(cfg *conf.Config, d *dao.DAO, dispatcher *Dispatcher, registry *SingletonRegistry, client redis.UniversalClient, locker *redis_lock.Client, producer *red_mq.Producer, defs *DefinitionCache, recoverWorkers int) *Monitor {
	if recoverWorkers <= 0 {
		recoverWorkers = 8
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Monitor{
		ctx: ctx, cancel: cancel,
		cfg:        cfg,
		dao:        d,
		dispatcher: dispatcher,
		defs:       defs,
		registry:   registry,
		client:     client,
		locker:     locker,
		producer:   producer,
		pool:       pool.NewGoWorkerPool(recoverWorkers),
		stopChan:   make(chan struct{}),
	}
}

func (m *Monitor) Start() {
	m.wheel = time_wheel.NewTimeWheel(32, 5*time.Second)
	m.wheel.AddTask(sweepKey, m.scheduleSweep, time.Now().Add(30*time.Second))
	slog.Info("monitor started", "node", m.cfg.Node.ID)
}

func (m *Monitor) Stop() {
	m.stopOnce.Do(func() { close(m.stopChan); m.cancel() })
	if m.wheel != nil {
		m.wheel.Stop()
	}
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if closable, ok := m.pool.(interface{ Close() }); ok {
		closable.Close()
	}
}

// scheduleSweep runs one sweep and re-arms itself on the in-process wheel.
func (m *Monitor) scheduleSweep() {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	select {
	case <-m.stopChan:
		return
	default:
	}

	m.sweep()

	select {
	case <-m.stopChan:
		return
	default:
		m.wheel.AddTask(sweepKey, m.scheduleSweep, time.Now().Add(30*time.Second))
	}
}

func (m *Monitor) sweep() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("monitor sweep panicked", "panic", r)
			obsx.CapturePanic(context.Background(), r, map[string]string{"role": "monitor"})
		}
	}()

	ctx, cancel := context.WithTimeout(m.ctx, 50*time.Second)
	defer cancel()

	if !m.registry.IsOwner(ctx, SingletonMonitor) {
		return
	}
	lock := redis_lock.NewRedisLock(monitorLockKey, m.locker, redis_lock.WithExpireSeconds(55))
	if err := lock.Lock(ctx); err != nil {
		return
	}
	defer func() { _ = lock.Unlock(ctx) }()

	m.failStuckRunning(ctx)
	m.redispatchOverdue(ctx)
	m.republishStuckQueued(ctx)
	m.syncData(ctx)
	m.publishStats(ctx)
}

// each runs fn over rows on the bounded worker pool and waits for completion
// so one sweep stays inside its lock budget.
func (m *Monitor) each(n int, fn func(i int)) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		if err := m.pool.Submit(func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("monitor pool task panicked", "panic", r)
					obsx.CapturePanic(context.Background(), r, map[string]string{"role": "monitor-pool"})
				}
			}()
			fn(i)
		}); err != nil {
			wg.Done()
			slog.Warn("monitor pool submit failed", "err", err)
		}
	}
	wg.Wait()
}

// failStuckRunning marks executions running far beyond the executor budget as
// failed; their node crashed or the LLM call wedged.
func (m *Monitor) failStuckRunning(ctx context.Context) {
	runningBefore := time.Now().Add(-time.Duration(m.cfg.Scheduler.StuckRunningMinutes) * time.Minute)

	stuck, err := m.dao.StuckExecutions(ctx, runningBefore)
	if err != nil {
		slog.Error("load stuck executions", "err", err)
		return
	}
	m.each(len(stuck), func(i int) {
		exec := stuck[i]
		affected, err := m.dao.Transition(ctx, exec.ID, []string{po.StatusRunning}, po.StatusFailed, map[string]any{
			"error":       "execution timed out (node crashed or LLM call hung); marked failed by monitor",
			"finished_at": time.Now(),
		})
		if err != nil || affected == 0 {
			return
		}
		slog.Warn("marked stuck execution failed", "execution_id", exec.ID, "node", exec.NodeID)
		obsx.CaptureError(ctx, fmt.Errorf("execution %d stuck in running on node %s", exec.ID, exec.NodeID), map[string]string{
			"execution_id": fmt.Sprintf("%d", exec.ID),
			"node":         exec.NodeID,
		})
	})
}

// republishStuckQueued re-publishes the exec command for queued executions
// whose message was lost or consumed before the TCC reserve landed. The row
// stays owned by the original transaction, so the executor's tx-id check
// keeps duplicate deliveries idempotent.
func (m *Monitor) republishStuckQueued(ctx context.Context) {
	if m.producer == nil {
		return
	}
	before := time.Now().Add(-time.Duration(m.cfg.Scheduler.OverdueRecoverMinutes) * time.Minute)
	rows, err := m.dao.QueuedStuck(ctx, before)
	if err != nil {
		slog.Error("load stuck queued executions", "err", err)
		return
	}
	m.each(len(rows), func(i int) {
		exec := rows[i]
		if exec.TxID == "" {
			return
		}
		cmd := ExecCommand{ExecutionID: exec.ID, TxID: exec.TxID, FireKey: exec.FireKey}
		body, err := json.Marshal(cmd)
		if err != nil {
			return
		}
		if _, err := m.producer.SendMsg(ctx, m.cfg.MQ.ExecTopic, exec.FireKey, string(body)); err != nil {
			slog.Warn("re-publish queued command failed", "execution_id", exec.ID, "err", err)
			return
		}
		slog.Info("re-published command for stuck queued execution", "execution_id", exec.ID, "tx_id", exec.TxID)
	})
}

// redispatchOverdue gives pending executions whose wheel callback never
// arrived (cluster downtime, redis shard aged out) another chance through
// the normal dispatch funnel.
func (m *Monitor) redispatchOverdue(ctx context.Context) {
	before := time.Now().Add(-time.Duration(m.cfg.Scheduler.OverdueRecoverMinutes) * time.Minute)
	overdue, err := m.dao.PendingOverdue(ctx, before)
	if err != nil {
		slog.Error("load overdue executions", "err", err)
		return
	}
	m.each(len(overdue), func(i int) {
		exec := overdue[i]
		dispatched, err := m.dispatcher.Dispatch(ctx, exec)
		if err != nil {
			slog.Warn("re-dispatch overdue execution failed", "execution_id", exec.ID, "err", err)
			return
		}
		if dispatched {
			slog.Info("re-dispatched overdue execution", "execution_id", exec.ID, "fire_key", exec.FireKey)
		}
	})
}

// syncData is the MySQL -> redis data-sync pass. It (1) mirrors the status of
// recently touched executions into the ExecMirrorKey hash so readers (and
// cross-store reconciliation) never hit MySQL for hot statuses, and (2) runs
// the definition-cache drift repair so redis-cached task definitions cannot
// diverge from the DB for longer than one sweep interval.
func (m *Monitor) syncData(ctx context.Context) {
	for batch := 0; batch < 4; batch++ {
		count, err := storage.SyncExecutionMirror(ctx, m.dao.DB(), m.client, ExecMirrorKey, ExecMirrorKey+":cursor", 500)
		if err != nil {
			slog.Warn("sync: execution mirror", "err", err)
			break
		}
		if count < 500 {
			break
		}
	}

	if m.defs != nil {
		if repaired, err := m.defs.SyncDrift(ctx); err != nil {
			slog.Warn("sync: definition drift repair", "err", err)
			obsx.CaptureError(ctx, fmt.Errorf("definition drift repair: %w", err), nil)
		} else if repaired > 0 {
			slog.Info("sync: repaired drifted definition cache entries", "count", repaired)
		}
	}
}

// StatusMirror exposes the redis execution-status mirror to the monitor API.
func (m *Monitor) StatusMirror(ctx context.Context) (map[string]string, error) {
	fields, _, err := m.client.HScan(ctx, ExecMirrorKey, 0, "*", 200).Result()
	vals := make(map[string]string)
	for i := 0; i+1 < len(fields) && len(vals) < 200; i += 2 {
		vals[fields[i]] = fields[i+1]
	}
	if err == redis.Nil {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return vals, nil
}

func (m *Monitor) publishStats(ctx context.Context) {
	dayStart := time.Now().UTC().Truncate(24 * time.Hour)
	statusCounts, err := m.dao.ExecutionStats(ctx, dayStart)
	if err != nil {
		slog.Error("execution stats", "err", err)
		return
	}

	scheduledEnabled, err := m.dao.EnabledScheduledTasks(ctx)
	if err != nil {
		return
	}
	conditionEnabled, err := m.dao.EnabledConditionTasks(ctx, "")
	if err != nil {
		return
	}
	mirrorSize, err := m.client.HLen(ctx, ExecMirrorKey).Result()
	if err != nil {
		mirrorSize = 0
	}

	stats := map[string]any{
		"node":                 m.cfg.Node.ID,
		"generated_at":         time.Now().UTC().Format(time.RFC3339),
		"today_executions":     statusCounts,
		"scheduled_enabled":    len(scheduledEnabled),
		"condition_enabled":    len(conditionEnabled),
		"monitor_owner":        m.registry.ownerOf(ctx, SingletonMonitor),
		"migrator_owner":       m.registry.ownerOf(ctx, SingletonMigrator),
		"migrate_step_minutes": m.cfg.Scheduler.MigrateStepMinutes,
		"exec_mirror_size":     mirrorSize,
	}
	body, err := json.Marshal(stats)
	if err != nil {
		return
	}
	if err := m.client.Set(ctx, StatsRedisKey, body, 2*time.Minute).Err(); err != nil {
		slog.Warn("publish stats", "err", err)
	}
}

// Stats reads the latest published snapshot.
func (m *Monitor) Stats(ctx context.Context) (map[string]any, error) {
	body, err := m.client.Get(ctx, StatsRedisKey).Result()
	if err == redis.Nil {
		return map[string]any{"available": false}, nil
	}
	if err != nil {
		return nil, err
	}
	var stats map[string]any
	if err := json.Unmarshal([]byte(body), &stats); err != nil {
		return nil, err
	}
	stats["available"] = true
	return stats, nil
}
