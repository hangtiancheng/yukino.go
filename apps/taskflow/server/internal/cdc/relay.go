package cdc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/obsx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/telemetry"
	"github.com/hangtiancheng/yukino.go/components/red_mq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Relay struct {
	db       *gorm.DB
	producer *red_mq.Producer
	topic    string
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewRelay(db *gorm.DB, producer *red_mq.Producer, topic string) *Relay {
	return &Relay{db: db, producer: producer, topic: topic}
}

func (r *Relay) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			if err := r.Drain(ctx, 100); err != nil && ctx.Err() == nil {
				slog.Error("change relay failed", "err", err)
				obsx.CaptureError(ctx, err, map[string]string{"role": "outbox"})
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

func (r *Relay) Stop() {
	if r.cancel != nil {
		r.cancel()
		r.wg.Wait()
	}
}

// Drain never advances a timestamp cursor: auto-increment IDs can commit out
// of order. SKIP LOCKED permits many replicas to drain independent rows.
func (r *Relay) Drain(ctx context.Context, budget int) error {
	for i := 0; i < budget; i++ {
		worked, err := r.fanout(ctx)
		if err != nil {
			return err
		}
		if !worked {
			break
		}
	}
	for i := 0; i < budget; i++ {
		worked, err := r.publish(ctx)
		if err != nil {
			return err
		}
		if !worked {
			break
		}
	}
	return nil
}

func (r *Relay) fanout(ctx context.Context) (worked bool, err error) {
	ctx, span := telemetry.Tracer("cdc").Start(ctx, "change.fanout")
	defer span.End()
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var change po.ChangeEvent
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("processed_at IS NULL").Order("id").Limit(1).Find(&change)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		worked = true
		if change.Operation == "insert" {
			var tasks []po.ConditionTask
			if err := tx.Where("enabled = ? AND table_name = ? AND created_at <= ?", true, change.SourceTable, change.OccurredAt).Find(&tasks).Error; err != nil {
				return err
			}
			for _, task := range tasks {
				key := fmt.Sprintf("cond:%d:change:%s", task.ID, change.EventKey)
				trigger, err := json.Marshal(map[string]any{"event_type": po.EventTypeMySQLInsert, "table": change.SourceTable, "event_key": change.EventKey, "occurred_at": change.OccurredAt, "record": json.RawMessage(change.RecordJSON)})
				if err != nil {
					return err
				}
				exec := &po.Execution{TaskType: po.TaskTypeCondition, TaskID: task.ID, TaskName: task.Name, FireKey: key, FireAt: change.OccurredAt, Status: po.StatusPending, TriggerInfo: string(trigger), PromptSnapshot: task.Prompt, ModelSnapshot: task.Model}
				_, id, err := dao.New(tx).InsertPending(ctx, exec)
				if err != nil {
					return err
				}
				payload, err := json.Marshal(map[string]any{"fire_key": key, "task_id": task.ID, "execution_id": id, "table": change.SourceTable, "record_json": change.RecordJSON, "occurred_at": change.OccurredAt.Format(time.RFC3339Nano), "trace_carrier": telemetry.Inject(ctx)})
				if err != nil {
					return err
				}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&po.Outbox{EventKey: key, Topic: r.topic, Payload: string(payload)}).Error; err != nil {
					return err
				}
			}
		}
		return tx.Model(&change).Update("processed_at", time.Now().UTC()).Error
	})
	return
}

func (r *Relay) publish(ctx context.Context) (worked bool, err error) {
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var event po.Outbox
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("published_at IS NULL").Order("id").Limit(1).Find(&event)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		worked = true
		publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if _, err := r.producer.SendMsg(publishCtx, event.Topic, event.EventKey, event.Payload); err != nil {
			return err
		}
		return tx.Model(&event).Update("published_at", time.Now().UTC()).Error
	})
	return
}
