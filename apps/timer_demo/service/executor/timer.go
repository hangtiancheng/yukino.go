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

package executor

import (
	"context"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/conf"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/consts"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/po"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/vo"
	task_dao "github.com/hangtiancheng/yukino.go/apps/timer_demo/dao/task"
	timer_dao "github.com/hangtiancheng/yukino.go/apps/timer_demo/dao/timer"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/log"
)

type TimerService struct {
	sync.Once
	confProvider *conf.MigratorAppConfProvider
	ctx          context.Context
	stop         func()
	// mu guards timers: the refresh goroutine replaces the map while
	// GetTimer reads it concurrently from request goroutines.
	mu       sync.RWMutex
	timers   map[uint]*vo.Timer
	timerDAO timerDAO
	taskDAO  *task_dao.TaskDAO
}

func NewTimerService(timerDAO *timer_dao.TimerDAO, taskDAO *task_dao.TaskDAO, confProvider *conf.MigratorAppConfProvider) *TimerService {
	return &TimerService{
		confProvider: confProvider,
		timers:       make(map[uint]*vo.Timer),
		timerDAO:     timerDAO,
		taskDAO:      taskDAO,
	}
}

func (t *TimerService) Start(ctx context.Context) {
	t.Do(func() {
		// Assign synchronously so Stop() never races the assignment or calls
		// a nil stop func when Stop happens right after Start.
		t.ctx, t.stop = context.WithCancel(ctx)

		go func() {
			stepMinutes := t.confProvider.Get().TimerDetailCacheMinutes
			ticker := time.NewTicker(time.Duration(stepMinutes) * time.Minute)
			defer ticker.Stop()

			for {
				select {
				case <-t.ctx.Done():
					return
				case <-ticker.C:
				}

				go t.refreshTimers(ctx, stepMinutes)
			}
		}()
	})
}

func (t *TimerService) refreshTimers(ctx context.Context, stepMinutes int) {
	start := time.Now()
	timers, err := t.getTimersByTime(ctx, start, start.Add(time.Duration(stepMinutes)*time.Minute))
	if err != nil {
		log.ErrorContextf(ctx, "refresh timers failed, start: %v, err: %v", start, err)
	}

	t.mu.Lock()
	t.timers = timers
	t.mu.Unlock()
}

func (t *TimerService) getTimersByTime(ctx context.Context, start, end time.Time) (map[uint]*vo.Timer, error) {
	tasks, err := t.taskDAO.GetTasks(ctx, task_dao.WithStartTime(start), task_dao.WithEndTime(end))
	if err != nil {
		return nil, err
	}

	timerIDs := getTimerIDs(tasks)
	if len(timerIDs) == 0 {
		return nil, nil
	}
	pTimers, err := t.timerDAO.GetTimers(ctx, timer_dao.WithIDs(timerIDs), timer_dao.WithStatus(int32(consts.Enable)))
	if err != nil {
		return nil, err
	}

	return getTimersMap(pTimers)
}

func getTimerIDs(tasks []*po.Task) []uint {
	timerIDSet := make(map[uint]struct{})
	for _, task := range tasks {
		if _, ok := timerIDSet[task.TimerID]; ok {
			continue
		}
		timerIDSet[task.TimerID] = struct{}{}
	}
	timerIDs := make([]uint, 0, len(timerIDSet))
	for id := range timerIDSet {
		timerIDs = append(timerIDs, id)
	}
	return timerIDs
}

func getTimersMap(pTimers []*po.Timer) (map[uint]*vo.Timer, error) {
	vTimers, err := vo.NewTimers(pTimers)
	if err != nil {
		return nil, err
	}

	timers := make(map[uint]*vo.Timer, len(vTimers))
	for _, vTimer := range vTimers {
		timers[vTimer.ID] = vTimer
	}
	return timers, nil
}

func (t *TimerService) GetTimer(ctx context.Context, id uint) (*vo.Timer, error) {
	t.mu.RLock()
	vTimer, ok := t.timers[id]
	t.mu.RUnlock()
	if ok {
		// log.InfoContextf(ctx, "get timer from local cache success, timer: %+v", vTimer)
		return vTimer, nil
	}

	log.WarnContextf(ctx, "get timer from local cache failed, timerID: %d", id)

	timer, err := t.timerDAO.GetTimer(ctx, timer_dao.WithID(id))
	if err != nil {
		return nil, err
	}

	return vo.NewTimer(timer)
}

func (t *TimerService) Stop() {
	if t.stop != nil {
		t.stop()
	}
}

type timerDAO interface {
	GetTimer(context.Context, ...timer_dao.Option) (*po.Timer, error)
	GetTimers(ctx context.Context, opts ...timer_dao.Option) ([]*po.Timer, error)
}
