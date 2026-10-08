package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/consensus"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/cronx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/idem"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/journal"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/obsx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/telemetry"
	"github.com/hangtiancheng/yukino.go/components/tcc"
)

// Dispatcher turns an execution row (status=pending) into a queued MQ command
// through one TCC transaction. It is the single funnel used by the time-wheel
// fire callback, condition events and the monitor recovery sweep, which keeps
// the idempotency guarantees identical across all trigger paths.
type Dispatcher struct {
	dao     *dao.DAO
	idem    *idem.Service
	bloom   *idem.BloomFilter
	journal *journal.Store
	ledger  *consensus.Ledger
	manager *tcc.TXManager
	mqConf  conf.MQConf
	// claimStuckThreshold bounds how long a redis claim may hide a pending
	// execution before another node is allowed to take over the dispatch.
	claimStuckThreshold time.Duration
}

func NewDispatcher(d *dao.DAO, i *idem.Service, bloom *idem.BloomFilter, manager *tcc.TXManager, mqConf conf.MQConf, jr *journal.Store, ledger *consensus.Ledger) *Dispatcher {
	return &Dispatcher{
		dao:                 d,
		idem:                i,
		bloom:               bloom,
		manager:             manager,
		mqConf:              mqConf,
		journal:             jr,
		ledger:              ledger,
		claimStuckThreshold: 5 * time.Minute,
	}
}

// Dispatch attempts to dispatch the execution. dispatched=false with err=nil
// means a duplicate trigger that was correctly suppressed.
func (d *Dispatcher) Dispatch(ctx context.Context, exec *po.Execution) (dispatched bool, err error) {
	ctx, span := telemetry.Tracer("dispatcher").Start(ctx, "execution.dispatch")
	defer span.End()
	if exec.FireAt.After(time.Now()) {
		return false, fmt.Errorf("execution is not due")
	}
	if exec.TaskType == po.TaskTypeScheduled && strings.HasPrefix(exec.FireKey, "sched:") {
		task, loadErr := d.dao.GetScheduledTask(ctx, exec.TaskID)
		if loadErr != nil && !dao.IsNotFound(loadErr) {
			return false, loadErr
		}
		if dao.IsNotFound(loadErr) || !task.Enabled || !matchesSchedule(task, exec.FireAt) {
			_, err = d.dao.Transition(ctx, exec.ID, []string{po.StatusPending}, po.StatusCancelled, map[string]any{"finished_at": time.Now(), "error": "scheduled definition is disabled, deleted or changed"})
			return false, err
		}
	}
	// Layer 0: timer bloom pre-check. A definite miss skips nothing (the
	// strong layers below still run); a probable hit is resolved against the
	// durable fire_key row before anything is suppressed.
	if d.bloom.MaybeSeen(ctx, exec.FireKey) {
		if existing, lerr := d.dao.GetExecutionByFireKey(ctx, exec.FireKey); lerr == nil &&
			existing.ID == exec.ID && existing.Status != po.StatusPending {
			slog.Info("duplicate fire suppressed by bloom pre-check",
				"execution_id", exec.ID, "fire_key", exec.FireKey, "status", existing.Status)
			return false, nil
		}
	}

	ok, err := d.idem.ClaimFire(ctx, exec.FireKey)
	if err != nil {
		return false, fmt.Errorf("claim fire key: %w", err)
	}
	if !ok {
		fresh, ferr := d.dao.GetExecution(ctx, exec.ID)
		if ferr != nil {
			return false, fmt.Errorf("reload execution %d: %w", exec.ID, ferr)
		}
		if fresh.Status == po.StatusPending && time.Since(fresh.UpdatedAt) > d.claimStuckThreshold {
			// Previous claimer crashed before dispatching; the DB row is the
			// source of truth and is still pending, so it is safe to race for
			// the reservation below.
			slog.Info("taking over stuck pending execution", "execution_id", exec.ID, "fire_key", exec.FireKey)
		} else {
			slog.Info("duplicate fire suppressed", "execution_id", exec.ID, "fire_key", exec.FireKey, "status", fresh.Status)
			return false, nil
		}
	}

	fresh, err := d.dao.GetExecution(ctx, exec.ID)
	if err != nil {
		d.releaseIfPending(ctx, exec)
		return false, fmt.Errorf("reload execution %d: %w", exec.ID, err)
	}
	if fresh.Status != po.StatusPending {
		// Already dispatched or finalized through another path.
		return false, nil
	}

	txID, tccOK, err := d.manager.Transaction(ctx,
		&tcc.RequestEntity{
			ComponentID: ComponentExecutionReserve,
			Request:     map[string]any{"execution_id": fresh.ID},
		},
		&tcc.RequestEntity{
			ComponentID: ComponentMQDispatch,
			Request:     map[string]any{"execution_id": fresh.ID, "fire_key": fresh.FireKey},
		},
	)
	if err != nil {
		obsx.CaptureError(ctx, fmt.Errorf("dispatch tx for execution %d: %w", fresh.ID, err), map[string]string{
			"fire_key": fresh.FireKey,
		})
		d.releaseIfPending(ctx, fresh)
		return false, err
	}
	if !tccOK {
		// The transaction cancelled inline; ExecutionReserveComponent.Cancel
		// rolled the row back to pending and released the claim.
		slog.Warn("dispatch tx cancelled", "execution_id", fresh.ID, "tx_id", txID)
		return false, nil
	}

	slog.Info("execution dispatched", "execution_id", fresh.ID, "tx_id", txID, "fire_key", fresh.FireKey)

	// Audit the dispatch through the timer bloom filter, the node-local
	// LSM journal and the embedded raft ledger. All three are best-effort and
	// must never fail the dispatch; the request context may already be gone by
	// the time a fire callback returns, so the writes run detached.
	auditCtx := context.WithoutCancel(ctx)
	d.bloom.MarkSeen(auditCtx, fresh.FireKey)
	if err := d.journal.RecordDispatch(fresh.FireKey, fresh.ID, fresh.TaskType, fresh.TaskName); err != nil {
		slog.Warn("journal dispatch audit failed", "fire_key", fresh.FireKey, "err", err)
	}
	if err := d.ledger.Propose(auditCtx, "dispatch:"+fresh.FireKey, fmt.Sprintf("%d", fresh.ID)); err != nil {
		slog.Warn("consensus propose failed", "fire_key", fresh.FireKey, "err", err)
	}
	return true, nil
}

func matchesSchedule(task *po.ScheduledTask, fireAt time.Time) bool {
	loc, err := time.LoadLocation(task.Timezone)
	if err != nil {
		return false
	}
	schedule, err := cronx.Parse(task.CronExpr)
	return err == nil && schedule.Next(fireAt.Add(-time.Nanosecond).In(loc)).Equal(fireAt)
}

// releaseIfPending drops the redis claim when the execution never left
// pending, so a recovery sweep is not blocked by a claim we still hold.
func (d *Dispatcher) releaseIfPending(ctx context.Context, exec *po.Execution) {
	fresh, err := d.dao.GetExecution(ctx, exec.ID)
	if err != nil {
		return
	}
	if fresh.Status == po.StatusPending {
		if err := d.idem.ReleaseFire(ctx, fresh.FireKey); err != nil {
			slog.Warn("release fire claim failed", "fire_key", fresh.FireKey, "err", err)
		}
	}
}
