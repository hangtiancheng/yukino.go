// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package timer

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/po"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/log"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/mysql"

	"gorm.io/gorm"
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
	return timer.ID, t.client.DB.WithContext(ctx).Create(timer).Error
}

func (t *TimerDAO) DeleteTimer(ctx context.Context, id uint) error {
	return t.client.DB.WithContext(ctx).Delete(&po.Timer{Model: gorm.Model{ID: id}}).Error
}

func (t *TimerDAO) UpdateTimer(ctx context.Context, timer *po.Timer) error {
	return t.client.DB.WithContext(ctx).Updates(timer).Error
}

func (t *TimerDAO) GetTimer(ctx context.Context, opts ...Option) (*po.Timer, error) {
	db := t.client.WithContext(ctx)
	for _, opt := range opts {
		db = opt(db)
	}
	var timer po.Timer
	return &timer, db.First(&timer).Error
}

func (t *TimerDAO) GetTimers(ctx context.Context, opts ...Option) ([]*po.Timer, error) {
	db := t.client.DB.WithContext(ctx).Model(&po.Timer{})
	for _, opt := range opts {
		db = opt(db)
	}
	var timers []*po.Timer
	return timers, db.Scan(&timers).Error
}

func (t *TimerDAO) Count(ctx context.Context, opts ...Option) (int64, error) {
	db := t.client.DB.WithContext(ctx).Model(&po.Timer{})
	for _, opt := range opts {
		db = opt(db)
	}
	var cnt int64
	return cnt, db.Debug().Count(&cnt).Error
}

func (t *TimerDAO) Transaction(ctx context.Context, do func(ctx context.Context, dao *TimerDAO) error) error {
	return t.client.Transaction(func(tx *gorm.DB) error {
		defer func() {
			if err := recover(); err != nil {
				tx.Rollback()
				log.ErrorContextf(ctx, "transaction err: %v", err)
			}
		}()
		return do(ctx, NewTimerDAO(mysql.NewClient(tx)))
	})
}

func (t *TimerDAO) BatchCreateRecords(ctx context.Context, tasks []*po.Task) error {
	return t.client.DB.Model(&po.Task{}).WithContext(ctx).CreateInBatches(tasks, len(tasks)).Error
}

func (t *TimerDAO) DoWithLock(ctx context.Context, id uint, do func(ctx context.Context, dao *TimerDAO, timer *po.Timer) error) error {
	return t.client.Transaction(func(tx *gorm.DB) error {
		defer func() {
			if err := recover(); err != nil {
				tx.Rollback()
				log.ErrorContextf(ctx, "transaction with lock err: %v, timer id: %d", err, id)
			}
		}()

		var timer po.Timer
		if err := tx.Set("gorm:query_option", "FOR UPDATE").WithContext(ctx).First(&timer, id).Error; err != nil {
			return err
		}

		return do(ctx, NewTimerDAO(mysql.NewClient(tx)), &timer)
	})
}
