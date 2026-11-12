package timer

import (
	"context"
	"errors"
	"fmt"

	"github.com/hangtiancheng/yukino.go/components/timer/common/model/po"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/log"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TimerDAO struct {
	client *mysql.Client
}

func NewTimerDAO(client *mysql.Client) *TimerDAO {
	return &TimerDAO{
		client: client,
	}
}

func (t *TimerDAO) CreateTimer(ctx context.Context, timer *po.Timer) (uint, error) {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil || timer == nil {
		return 0, errors.New("timer dao, context, and timer are required")
	}
	return timer.ID, t.client.DB.WithContext(ctx).Create(timer).Error
}

func (t *TimerDAO) DeleteTimer(ctx context.Context, id uint) error {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil {
		return errors.New("timer dao and context are required")
	}
	return t.client.DB.WithContext(ctx).Delete(&po.Timer{Model: gorm.Model{ID: id}}).Error
}

func (t *TimerDAO) UpdateTimer(ctx context.Context, timer *po.Timer) error {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil || timer == nil {
		return errors.New("timer dao, context, and timer are required")
	}
	return t.client.DB.WithContext(ctx).Updates(timer).Error
}

func (t *TimerDAO) GetTimer(ctx context.Context, opts ...Option) (*po.Timer, error) {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil {
		return nil, errors.New("timer dao and context are required")
	}
	db := t.client.WithContext(ctx)
	for _, opt := range opts {
		if opt != nil {
			db = opt(db)
		}
	}
	var timer po.Timer
	return &timer, db.First(&timer).Error
}

func (t *TimerDAO) GetTimers(ctx context.Context, opts ...Option) ([]*po.Timer, error) {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil {
		return nil, errors.New("timer dao and context are required")
	}
	db := t.client.DB.WithContext(ctx).Model(&po.Timer{})
	for _, opt := range opts {
		if opt != nil {
			db = opt(db)
		}
	}
	var timers []*po.Timer
	return timers, db.Scan(&timers).Error
}

func (t *TimerDAO) Count(ctx context.Context, opts ...Option) (int64, error) {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil {
		return 0, errors.New("timer dao and context are required")
	}
	db := t.client.DB.WithContext(ctx).Model(&po.Timer{})
	for _, opt := range opts {
		if opt != nil {
			db = opt(db)
		}
	}
	var cnt int64
	return cnt, db.Count(&cnt).Error
}

func (t *TimerDAO) Transaction(ctx context.Context, do func(ctx context.Context, dao *TimerDAO) error) error {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil || do == nil {
		return errors.New("timer dao, context, and transaction callback are required")
	}
	return t.client.Transaction(func(tx *gorm.DB) (callbackErr error) {
		defer func() {
			if fault := recover(); fault != nil {
				tx.Rollback()
				callbackErr = fmt.Errorf("timer transaction panicked: %v", fault)
				log.ErrorContextf(ctx, "%v", callbackErr)
			}
		}()
		return do(ctx, NewTimerDAO(mysql.NewClient(tx)))
	})
}

func (t *TimerDAO) BatchCreateRecords(ctx context.Context, tasks []*po.Task) error {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil {
		return errors.New("timer dao and context are required")
	}
	if len(tasks) == 0 {
		return nil
	}
	for _, task := range tasks {
		if task == nil {
			return errors.New("nil task")
		}
	}
	return t.client.DB.Model(&po.Task{}).WithContext(ctx).CreateInBatches(tasks, len(tasks)).Error
}

func (t *TimerDAO) DoWithLock(ctx context.Context, id uint, do func(ctx context.Context, dao *TimerDAO, timer *po.Timer) error) error {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil || do == nil {
		return errors.New("timer dao, context, and callback are required")
	}
	return t.client.Transaction(func(tx *gorm.DB) (callbackErr error) {
		defer func() {
			if fault := recover(); fault != nil {
				tx.Rollback()
				callbackErr = fmt.Errorf("timer transaction with lock panicked for timer %d: %v", id, fault)
				log.ErrorContextf(ctx, "%v", callbackErr)
			}
		}()

		var timer po.Timer
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).WithContext(ctx).First(&timer, id).Error; err != nil {
			return err
		}

		return do(ctx, NewTimerDAO(mysql.NewClient(tx)), &timer)
	})
}
