package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/consensus"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/journal"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/obsx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/telemetry"
	"github.com/hangtiancheng/yukino.go/components/red_mq"
	mqredis "github.com/hangtiancheng/yukino.go/components/red_mq/redis"
)

// Executor consumes execution commands from red_mq and runs the LLM agent.
// Multiple consumer instances per node share one consumer group; red_mq
// retries failed callbacks and routes poison messages to the dead-letter
// table after maxRetry.
type Executor struct {
	cfg       *conf.Config
	dao       *dao.DAO
	agent     *llm.Agent
	reports   *ReportStore
	defs      *DefinitionCache
	client    *mqredis.Client
	mailbox   *DeadLetterMailbox
	journal   *journal.Store
	ledger    *consensus.Ledger
	consumers []*red_mq.Consumer
}

func NewExecutor(cfg *conf.Config, d *dao.DAO, agent *llm.Agent, reports *ReportStore, defs *DefinitionCache, client *mqredis.Client, mailbox *DeadLetterMailbox, jr *journal.Store, ledger *consensus.Ledger) *Executor {
	return &Executor{
		cfg:     cfg,
		dao:     d,
		agent:   agent,
		reports: reports,
		defs:    defs,
		client:  client,
		mailbox: mailbox,
		journal: jr,
		ledger:  ledger,
	}
}

func (e *Executor) Start(workers int) error {
	for i := 0; i < workers; i++ {
		consumerID := fmt.Sprintf("%s-exec-%d", e.cfg.Node.ID, i)
		consumer, err := red_mq.NewConsumer(
			e.client,
			e.cfg.MQ.ExecTopic,
			e.cfg.MQ.ExecGroup,
			consumerID,
			e.handle,
			red_mq.WithMaxRetryLimit(e.cfg.MQ.MaxRetry),
			red_mq.WithReceiveTimeout(time.Duration(e.cfg.Executor.ReceiveTimeoutMs)*time.Millisecond),
			red_mq.WithHandleMsgsTimeout(time.Duration(e.cfg.Executor.HandleTimeoutSeconds)*time.Second),
			red_mq.WithAbandonedMessageRecovery(time.Duration(e.cfg.Executor.HandleTimeoutSeconds+60)*time.Second),
			red_mq.WithDeadLetterMailbox(e.mailbox.ForTopic(e.cfg.MQ.ExecTopic)),
		)
		if err != nil {
			e.Stop()
			return fmt.Errorf("start exec consumer %s: %w", consumerID, err)
		}
		e.consumers = append(e.consumers, consumer)
	}
	slog.Info("executor consumers started", "topic", e.cfg.MQ.ExecTopic, "workers", workers)
	return nil
}

func (e *Executor) Stop() {
	for _, consumer := range e.consumers {
		consumer.Stop()
	}
	e.consumers = nil
}

func (e *Executor) handle(ctx context.Context, msg *mqredis.MsgEntity) error {
	var cmd ExecCommand
	if err := json.Unmarshal([]byte(msg.Val), &cmd); err != nil {
		return fmt.Errorf("malformed execution command: %w", err)
	}

	ctx = telemetry.Extract(ctx, cmd.TraceCarrier)
	ctx, span := telemetry.Tracer("executor").Start(ctx, "execution.run")
	defer span.End()

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(e.cfg.Executor.TimeoutSeconds)*time.Second)
	defer cancel()

	exec, err := e.awaitSettled(runCtx, cmd)
	if err != nil {
		return err
	}
	if exec == nil {
		// Duplicate, cancelled or stale command: consumed without running.
		return nil
	}

	if cmd.FireKey != exec.FireKey {
		return fmt.Errorf("execution command fire key mismatch")
	}
	affected, err := e.dao.TransitionOwned(runCtx, exec.ID, cmd.TxID, []string{po.StatusQueued}, po.StatusRunning, map[string]any{
		"started_at": time.Now(),
		"node_id":    e.cfg.Node.ID,
		"trace_id":   telemetry.TraceIDFrom(runCtx),
	})
	if err != nil {
		return fmt.Errorf("transition execution %d to running: %w", exec.ID, err)
	}
	if affected == 0 {
		// Somebody else (another delivery of the same command) won the race.
		return nil
	}

	e.runExecution(runCtx, exec)
	return nil
}

// awaitSettled waits for the dispatch transaction to become visible on the
// execution row. The TCC Try phase runs its participants concurrently, so the
// MQ command can be delivered before the reserve UPDATE lands; without this
// wait the executor would misread a legitimate command as stale.
// A (nil, nil) result means the message is consumed without running
// (duplicate / cancelled / stale); an error triggers red_mq retry accounting.
func (e *Executor) awaitSettled(ctx context.Context, cmd ExecCommand) (*po.Execution, error) {
	const attempts = 12
	for i := 0; i < attempts; i++ {
		exec, err := e.dao.GetExecution(ctx, cmd.ExecutionID)
		if err != nil {
			return nil, fmt.Errorf("load execution %d: %w", cmd.ExecutionID, err)
		}

		switch {
		case exec.Status == po.StatusCancelled:
			return nil, nil
		case exec.Status == po.StatusRunning, exec.Status == po.StatusSucceeded, exec.Status == po.StatusFailed:
			// Duplicate delivery of an already consumed command.
			return nil, nil
		case exec.Status == po.StatusPending && exec.TxID == "":
			// The reserve Try of our transaction has not landed yet; reload.
		case exec.Status == po.StatusPending:
			// The row was rolled back or re-reserved by another transaction;
			// this command must not run.
			slog.Info("exec command superseded, execution back in flight", "execution_id", exec.ID, "msg_tx", cmd.TxID, "row_tx", exec.TxID)
			return nil, nil
		case exec.TxID != cmd.TxID:
			slog.Info("stale exec command ignored", "execution_id", exec.ID, "msg_tx", cmd.TxID, "row_tx", exec.TxID)
			return nil, nil
		case exec.Status == po.StatusReserved:
			// Owned by our transaction but not confirmed yet; reload.
		case exec.Status == po.StatusQueued:
			return exec, nil
		default:
			return nil, fmt.Errorf("unexpected execution status %s for execution %d", exec.Status, exec.ID)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("execution %d not settled after wait window (cmd tx %s)", cmd.ExecutionID, cmd.TxID)
}

func (e *Executor) runExecution(ctx context.Context, exec *po.Execution) {
	startedAt := time.Now()
	tags := map[string]string{
		"execution_id": fmt.Sprintf("%d", exec.ID),
		"task_type":    exec.TaskType,
		"fire_key":     exec.FireKey,
	}
	defer func() {
		if fault := recover(); fault != nil {
			obsx.CapturePanic(ctx, fault, tags)
			e.finalize(ctx, exec, nil, startedAt, fmt.Errorf("execution panic: %v", fault), tags)
		}
	}()

	def, err := e.defs.LoadDefinition(ctx, exec)
	if err != nil {
		e.finalize(ctx, exec, nil, startedAt, fmt.Errorf("load task definition: %w", err), tags)
		return
	}

	model := def.Model
	if model == "" {
		model = e.cfg.LLM.Model()
	}
	exec.ModelSnapshot = model

	result, runErr := e.agent.Run(ctx, llm.RunInput{
		SystemPrompt: def.Prompt,
		UserPrompt:   BuildUserPrompt(exec),
		Model:        model,
	})
	if runErr != nil {
		e.finalize(ctx, exec, result, startedAt, runErr, tags)
		return
	}
	e.finalize(ctx, exec, result, startedAt, nil, tags)
}

func (e *Executor) finalize(ctx context.Context, exec *po.Execution, result *llm.RunResult, startedAt time.Time, runErr error, tags map[string]string) {
	// Persistence must survive an execution deadline or consumer shutdown.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	finishedAt := time.Now()
	status := po.StatusSucceeded
	if runErr != nil {
		status = po.StatusFailed
	}

	updates := map[string]any{
		"finished_at": finishedAt,
	}
	if result != nil {
		updates["tool_calls"] = len(result.ToolCalls)
		updates["tokens_prompt"] = result.TokensPrompt
		updates["tokens_output"] = result.TokensOutput
	}
	if runErr != nil {
		updates["error"] = runErr.Error()
		obsx.CaptureError(ctx, runErr, tags)
	}

	reportIn := ReportInput{
		Execution: exec,
		Model:     exec.ModelSnapshot,
		TraceID:   telemetry.TraceIDFrom(ctx),
		Result:    result,
		RunErr:    runErr,
		StartedAt: startedAt,
	}
	path, body, reportErr := e.reports.Render(reportIn)
	if reportErr != nil {
		err := reportErr
		slog.Error("render report", "execution_id", exec.ID, "err", err)
		if runErr == nil {
			runErr = fmt.Errorf("render report: %w", err)
			status = po.StatusFailed
			updates["error"] = runErr.Error()
		}
	} else {
		updates["report_path"] = path
		updates["report_body"] = body
		updates["report_excerpt"] = excerpt(body, 280)
	}

	from := []string{po.StatusRunning}
	affected, err := e.dao.TransitionOwned(ctx, exec.ID, exec.TxID, from, status, updates)
	if err != nil {
		slog.Error("finalize execution", "execution_id", exec.ID, "err", err)
		obsx.CaptureError(ctx, err, tags)
		return
	}
	if affected == 0 {
		slog.Warn("finalize skipped, execution moved on", "execution_id", exec.ID, "want", status)
		return
	}
	if reportErr == nil {
		e.reports.Warm(ctx, exec.ID, body)
	}

	// Fold the outcome into the LSM journal and the raft ledger. The redis
	// status mirror is kept by the monitor's sync sweeper. Audit writes are
	// best-effort and detached from the consumer context.
	auditCtx := context.WithoutCancel(ctx)
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	if err := e.journal.RecordOutcome(exec.FireKey, exec.ID, status, errText); err != nil {
		slog.Warn("journal outcome audit failed", "fire_key", exec.FireKey, "err", err)
	}
	if err := e.ledger.Propose(auditCtx, fmt.Sprintf("finish:%d", exec.ID), status); err != nil {
		slog.Warn("consensus finish propose failed", "execution_id", exec.ID, "err", err)
	}

	if status == po.StatusSucceeded && exec.TaskType == po.TaskTypeScheduled {
		if err := e.dao.DB().WithContext(ctx).Model(&po.ScheduledTask{}).
			Where("id = ?", exec.TaskID).
			Update("last_fire_at", exec.FireAt).Error; err != nil {
			slog.Warn("update last_fire_at", "task_id", exec.TaskID, "err", err)
		}
	}

	slog.Info("execution finished", "execution_id", exec.ID, "status", status,
		"duration_ms", finishedAt.Sub(startedAt).Milliseconds())
}
