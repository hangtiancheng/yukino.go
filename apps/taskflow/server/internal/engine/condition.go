package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/conf"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/idem"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/obsx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/telemetry"
	"github.com/hangtiancheng/yukino.go/components/red_mq"
	mqredis "github.com/hangtiancheng/yukino.go/components/red_mq/redis"
)

// CondEvent is the payload published on the condition topic when a business
// row is inserted. FireKey makes every (task, row) pair fire exactly once.
type CondEvent struct {
	ExecutionID  uint              `json:"execution_id,omitempty"`
	FireKey      string            `json:"fire_key"`
	TaskID       uint              `json:"task_id"`
	Table        string            `json:"table"`
	RecordID     uint              `json:"record_id"`
	RecordJSON   string            `json:"record_json"`
	OccurredAt   string            `json:"occurred_at"`
	TraceCarrier map[string]string `json:"trace_carrier,omitempty"`
}

// ConditionPipeline publishes insert events and converts them into execution
// rows that go through the exact same idempotent dispatch funnel as scheduled
// fires.
type ConditionPipeline struct {
	cfg        *conf.Config
	dao        *dao.DAO
	idem       *idem.Service
	producer   *red_mq.Producer
	dispatcher *Dispatcher
	defs       *DefinitionCache
	client     *mqredis.Client
	mailbox    *DeadLetterMailbox
	consumers  []*red_mq.Consumer
}

func NewConditionPipeline(cfg *conf.Config, d *dao.DAO, i *idem.Service, producer *red_mq.Producer, dispatcher *Dispatcher, defs *DefinitionCache, client *mqredis.Client, mailbox *DeadLetterMailbox) *ConditionPipeline {
	return &ConditionPipeline{
		cfg:        cfg,
		dao:        d,
		idem:       i,
		producer:   producer,
		dispatcher: dispatcher,
		defs:       defs,
		client:     client,
		mailbox:    mailbox,
	}
}

func CondFireKey(taskID uint, table string, recordID uint, createdAt time.Time) string {
	return fmt.Sprintf("cond:%d:%s:%d:%d", taskID, table, recordID, createdAt.Unix())
}

// PublishRecordInserted fans a freshly inserted business row out to every
// enabled condition task watching that table. Called by the API layer right
// after the INSERT commits.
func (p *ConditionPipeline) PublishRecordInserted(ctx context.Context, table string, recordID uint, record any, createdAt time.Time) (int, error) {
	tasks, err := p.dao.EnabledConditionTasks(ctx, table)
	if err != nil {
		return 0, fmt.Errorf("load condition tasks for %s: %w", table, err)
	}
	if len(tasks) == 0 {
		return 0, nil
	}

	recordJSON, err := json.Marshal(record)
	if err != nil {
		return 0, fmt.Errorf("marshal record: %w", err)
	}

	published := 0
	for _, task := range tasks {
		fireKey := CondFireKey(task.ID, table, recordID, createdAt)

		ok, err := p.idem.ClaimEvent(ctx, fireKey)
		if err != nil {
			slog.Error("claim condition event", "fire_key", fireKey, "err", err)
			continue
		}
		if !ok {
			slog.Info("duplicate condition event suppressed", "fire_key", fireKey)
			continue
		}

		event := CondEvent{
			FireKey:      fireKey,
			TaskID:       task.ID,
			Table:        table,
			RecordID:     recordID,
			RecordJSON:   string(recordJSON),
			OccurredAt:   time.Now().Format(time.RFC3339),
			TraceCarrier: telemetry.Inject(ctx),
		}
		body, err := json.Marshal(event)
		if err != nil {
			continue
		}
		if _, err := p.producer.SendMsg(ctx, p.cfg.MQ.CondTopic, fireKey, string(body)); err != nil {
			// Release the claim so a publisher retry can still deliver.
			_ = p.idem.ReleaseEvent(ctx, fireKey)
			obsx.CaptureError(ctx, fmt.Errorf("publish condition event %s: %w", fireKey, err), nil)
			continue
		}
		published++
	}
	return published, nil
}

func (p *ConditionPipeline) Start(workers int) error {
	for i := 0; i < workers; i++ {
		consumerID := fmt.Sprintf("%s-cond-%d", p.cfg.Node.ID, i)
		consumer, err := red_mq.NewConsumer(
			p.client,
			p.cfg.MQ.CondTopic,
			p.cfg.MQ.CondGroup,
			consumerID,
			p.handle,
			red_mq.WithMaxRetryLimit(p.cfg.MQ.MaxRetry),
			red_mq.WithReceiveTimeout(time.Duration(p.cfg.Executor.ReceiveTimeoutMs)*time.Millisecond),
			red_mq.WithHandleMsgsTimeout(time.Duration(p.cfg.Executor.HandleTimeoutSeconds)*time.Second),
			red_mq.WithAbandonedMessageRecovery(time.Duration(p.cfg.Executor.HandleTimeoutSeconds+60)*time.Second),
			red_mq.WithDeadLetterMailbox(p.mailbox.ForTopic(p.cfg.MQ.CondTopic)),
		)
		if err != nil {
			p.Stop()
			return fmt.Errorf("start cond consumer %s: %w", consumerID, err)
		}
		p.consumers = append(p.consumers, consumer)
	}
	slog.Info("condition consumers started", "topic", p.cfg.MQ.CondTopic, "workers", workers)
	return nil
}

func (p *ConditionPipeline) Stop() {
	for _, consumer := range p.consumers {
		consumer.Stop()
	}
	p.consumers = nil
}

func (p *ConditionPipeline) handle(ctx context.Context, msg *mqredis.MsgEntity) error {
	var event CondEvent
	if err := json.Unmarshal([]byte(msg.Val), &event); err != nil {
		slog.Error("malformed condition event, dropping", "msg_id", msg.MsgID, "err", err)
		return nil
	}

	ctx = telemetry.Extract(ctx, event.TraceCarrier)
	ctx, span := telemetry.Tracer("condition").Start(ctx, "condition.event")
	defer span.End()
	if event.ExecutionID != 0 {
		exec, err := p.dao.GetExecution(ctx, event.ExecutionID)
		if err != nil {
			return err
		}
		if exec.FireKey != event.FireKey || exec.TaskID != event.TaskID {
			return fmt.Errorf("condition event ownership mismatch")
		}
		if exec.Status != po.StatusPending {
			return nil
		}
		_, err = p.dispatcher.Dispatch(ctx, exec)
		return err
	}

	task, err := p.dao.GetConditionTask(ctx, event.TaskID)
	if err != nil {
		if dao.IsNotFound(err) {
			slog.Warn("condition task deleted, dropping event", "task_id", event.TaskID)
			return nil
		}
		return fmt.Errorf("load condition task %d: %w", event.TaskID, err)
	}
	if !task.Enabled {
		slog.Info("condition task disabled, dropping event", "task_id", task.ID)
		return nil
	}

	// Re-read the authoritative row so the analysis always reflects the
	// stored state, not the publisher's view.
	record, err := p.dao.GetRiskRecord(ctx, event.RecordID)
	if err != nil {
		if dao.IsNotFound(err) {
			slog.Warn("record gone before condition analysis", "record_id", event.RecordID)
			return nil
		}
		return fmt.Errorf("load record %d: %w", event.RecordID, err)
	}

	occurredAt, err := time.Parse(time.RFC3339, event.OccurredAt)
	if err != nil {
		occurredAt = time.Now()
	}

	triggerInfo, _ := json.Marshal(map[string]any{
		"event_type":  po.EventTypeMySQLInsert,
		"table":       event.Table,
		"record_id":   record.ID,
		"occurred_at": event.OccurredAt,
		"record":      record,
	})

	fireAt := record.CreatedAt
	if fireAt.IsZero() {
		fireAt = occurredAt
	}

	exec := &po.Execution{
		TaskType:    po.TaskTypeCondition,
		TaskID:      task.ID,
		TaskName:    task.Name,
		FireKey:     event.FireKey,
		Status:      po.StatusPending,
		FireAt:      fireAt,
		TriggerInfo: string(triggerInfo),
	}
	_, id, err := p.dao.InsertPending(ctx, exec)
	if err != nil {
		return fmt.Errorf("insert condition execution: %w", err)
	}
	exec.ID = id

	fresh, err := p.dao.GetExecution(ctx, id)
	if err != nil {
		return err
	}
	if fresh.Status != po.StatusPending {
		slog.Info("condition execution already handled", "execution_id", id, "status", fresh.Status)
		return nil
	}

	if _, err := p.dispatcher.Dispatch(ctx, fresh); err != nil {
		return fmt.Errorf("dispatch condition execution %d: %w", id, err)
	}
	return nil
}
