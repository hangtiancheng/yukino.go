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

package webserver

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/conf"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/consts"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/po"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/vo"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/utils"
	task_dao "github.com/hangtiancheng/yukino.go/apps/timer_demo/dao/task"
	timer_dao "github.com/hangtiancheng/yukino.go/apps/timer_demo/dao/timer"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/cron"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/log"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/mysql"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/redis"
)

const defaultEnableGapSeconds = 3

type TimerService struct {
	dao                 timerDAO
	confProvider        confProvider
	migrateConfProvider *conf.MigratorAppConfProvider
	cronParser          cronParser
	taskCache           taskCache
	lockService         *redis.Client
}

func NewTimerService(dao *timer_dao.TimerDAO, taskCache *task_dao.TaskCache, lockService *redis.Client,
	confProvider *conf.WebServerAppConfProvider, migrateConfProvider *conf.MigratorAppConfProvider, parser *cron.CronParser) *TimerService {
	return &TimerService{
		dao:                 dao,
		confProvider:        confProvider,
		migrateConfProvider: migrateConfProvider,
		taskCache:           taskCache,
		cronParser:          parser,
		lockService:         lockService,
	}
}

func (t *TimerService) CreateTimer(ctx context.Context, timer *vo.Timer) (uint, error) {
	lock := t.lockService.GetDistributionLock(utils.GetCreateLockKey(timer.App))
	if err := lock.Lock(ctx, defaultEnableGapSeconds); err != nil {
		return 0, errors.New("create/delete operations too frequent, please try again later")
	}
	// Validate the cron expression
	if !t.cronParser.IsValidCronExpr(timer.Cron) {
		return 0, fmt.Errorf("invalid cron expression: %s", timer.Cron)
	}

	pTimer, err := timer.ToPO()
	if err != nil {
		return 0, err
	}
	return t.dao.CreateTimer(ctx, pTimer)
}

func (t *TimerService) DeleteTimer(ctx context.Context, app string, id uint) error {
	lock := t.lockService.GetDistributionLock(utils.GetCreateLockKey(app))
	if err := lock.Lock(ctx, defaultEnableGapSeconds); err != nil {
		return errors.New("create/delete operations too frequent, please try again later")
	}
	return t.dao.DeleteTimer(ctx, id)
}

func (t *TimerService) UpdateTimer(ctx context.Context, timer *vo.Timer) error {
	pTimer, err := timer.ToPO()
	if err != nil {
		return err
	}
	return t.dao.UpdateTimer(ctx, pTimer)
}

func (t *TimerService) GetTimer(ctx context.Context, id uint) (*vo.Timer, error) {
	pTimer, err := t.dao.GetTimer(ctx, timer_dao.WithID(id))
	if err != nil {
		return nil, err
	}

	return vo.NewTimer(pTimer)
}

func (t *TimerService) EnableTimer(ctx context.Context, app string, id uint) error {
	// Rate limit enable/disable operations
	lock := t.lockService.GetDistributionLock(utils.GetEnableLockKey(app))
	if err := lock.Lock(ctx, defaultEnableGapSeconds); err != nil {
		return errors.New("enable/disable operations too frequent, please try again later")
	}

	do := func(ctx context.Context, dao *timer_dao.TimerDAO, timer *po.Timer) error {
		// Status validation
		if timer.Status != consts.Unable.ToInt() {
			return fmt.Errorf("not unable status, enable failed, timer id: %d", id)
		}

		// Get batch execution times
		// end is the right boundary of the next two migration slices
		start := time.Now()
		end := utils.GetForwardTwoMigrateStepEnd(start, 2*time.Duration(t.migrateConfProvider.Get().MigrateStepMinutes)*time.Minute)
		executeTimes, err := t.cronParser.NextsBefore(timer.Cron, end)
		if err != nil {
			log.ErrorContextf(ctx, "get executeTimes failed, err: %v", err)
			return err
		}

		// Add execution times to the database
		tasks := timer.BatchTasksFromTimer(executeTimes)
		// Use timer_id + run_timer unique key to prevent duplicate task insertion
		if err := dao.BatchCreateRecords(ctx, tasks); err != nil && !mysql.IsDuplicateEntryErr(err) {
			return err
		}

		// Add execution times to the Redis sorted set
		if err := t.taskCache.BatchCreateTasks(ctx, tasks, start, end); err != nil {
			return err
		}

		// Update timer status to enabled
		timer.Status = consts.Enable.ToInt()
		return dao.UpdateTimer(ctx, timer)
	}

	return t.dao.DoWithLock(ctx, id, do)
}

func (t *TimerService) UnableTimer(ctx context.Context, app string, id uint) error {
	// Rate limit enable/disable operations
	lock := t.lockService.GetDistributionLock(utils.GetEnableLockKey(app))
	if err := lock.Lock(ctx, defaultEnableGapSeconds); err != nil {
		return errors.New("enable/disable operations too frequent, please try again later")
	}

	do := func(ctx context.Context, dao *timer_dao.TimerDAO, timer *po.Timer) error {
		// Status validation
		if timer.Status != consts.Enable.ToInt() {
			return fmt.Errorf("not enabled status, unable failed, timer id: %d", id)
		}

		// Update timer status to disabled
		timer.Status = consts.Unable.ToInt()
		return dao.UpdateTimer(ctx, timer)
	}

	return t.dao.DoWithLock(ctx, id, do)
}

func (t *TimerService) GetAppTimers(ctx context.Context, req *vo.GetAppTimersReq) ([]*vo.Timer, int64, error) {
	total, err := t.dao.Count(ctx, timer_dao.WithApp(req.App))
	if err != nil {
		return nil, -1, err
	}

	offset, limit := req.Get()
	if total <= int64(offset) {
		return []*vo.Timer{}, total, nil
	}

	timers, err := t.dao.GetTimers(ctx, timer_dao.WithApp(req.App), timer_dao.WithPageLimit(offset, limit), timer_dao.WithDesc())
	if err != nil {
		return nil, -1, err
	}

	sort.Slice(timers, func(i, j int) bool {
		return timers[i].ID > timers[j].ID
	})

	vTimers, err := vo.NewTimers(timers)
	return vTimers, total, err
}

func (t *TimerService) GetTimersByName(ctx context.Context, req *vo.GetTimersByNameReq) ([]*vo.Timer, int64, error) {
	total, err := t.dao.Count(ctx, timer_dao.WithApp(req.App), timer_dao.WithFuzzyName(req.FuzzyName))
	if err != nil {
		return nil, -1, err
	}

	offset, limit := req.Get()
	if total <= int64(offset) {
		return []*vo.Timer{}, total, nil
	}

	timers, err := t.dao.GetTimers(ctx, timer_dao.WithApp(req.App), timer_dao.WithPageLimit(offset, limit), timer_dao.WithFuzzyName(req.FuzzyName))
	if err != nil {
		return nil, -1, err
	}

	sort.Slice(timers, func(i, j int) bool {
		return timers[i].ID > timers[j].ID
	})

	vTimers, err := vo.NewTimers(timers)
	return vTimers, total, err
}

type timerDAO interface {
	CreateTimer(ctx context.Context, timer *po.Timer) (uint, error)
	DeleteTimer(ctx context.Context, id uint) error
	UpdateTimer(ctx context.Context, timer *po.Timer) error
	GetTimer(ctx context.Context, opts ...timer_dao.Option) (*po.Timer, error)
	BatchCreateRecords(ctx context.Context, tasks []*po.Task) error
	DoWithLock(ctx context.Context, id uint, do func(ctx context.Context, dao *timer_dao.TimerDAO, timer *po.Timer) error) error
	GetTimers(ctx context.Context, opts ...timer_dao.Option) ([]*po.Timer, error)
	Count(ctx context.Context, opts ...timer_dao.Option) (int64, error)
}

type confProvider interface {
	Get() *conf.WebServerAppConf
}

type taskCache interface {
	BatchCreateTasks(ctx context.Context, tasks []*po.Task, start, end time.Time) error
}

type cronParser interface {
	NextsBefore(cron string, end time.Time) ([]time.Time, error)
	IsValidCronExpr(cron string) bool
}
