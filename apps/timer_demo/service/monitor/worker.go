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

package monitor

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/consts"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/utils"
	task_dao "github.com/hangtiancheng/yukino.go/apps/timer_demo/dao/task"
	timer_dao "github.com/hangtiancheng/yukino.go/apps/timer_demo/dao/timer"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/log"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/promethus"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/redis"
)

type Worker struct {
	lockService *redis.Client
	taskDAO     *task_dao.TaskDAO
	timerDAO    *timer_dao.TimerDAO
	reporter    *promethus.Reporter
}

func NewWorker(taskDAO *task_dao.TaskDAO, timerDAO *timer_dao.TimerDAO, lockService *redis.Client, reporter *promethus.Reporter) *Worker {
	return &Worker{
		taskDAO:     taskDAO,
		timerDAO:    timerDAO,
		lockService: lockService,
		reporter:    reporter,
	}
}

// Start reports the count of unexecuted timers every minute.
func (w *Worker) Start(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		now := time.Now()
		lock := w.lockService.GetDistributionLock(utils.GetMonitorLockKey(now))
		if err := lock.Lock(ctx, 2*int64(time.Minute/time.Second)); err != nil {
			continue
		}

		// Query timers from the previous minute
		minute := utils.GetMinute(now)
		go w.reportNoExceedTasksCnt(ctx, minute)
		go w.reportEnabledTimersCnt(ctx)
	}
}

func (w *Worker) reportNoExceedTasksCnt(ctx context.Context, minute time.Time) {
	noExceedTasksCnt, err := w.taskDAO.Count(ctx, task_dao.WithStartTime(minute.Add(-time.Minute)), task_dao.WithEndTime(minute), task_dao.WithStatus(int32(consts.NotRun)))
	if err != nil {
		log.ErrorContextf(ctx, "[monitor] get no exceed tasks cnt failed, err: %v", err)
		return
	}
	w.reporter.ReportTimerNoExceedRecord(float64(noExceedTasksCnt))
	log.InfoContextf(ctx, "[monitor] report no exceed tasks cnt success, cnt: %d", noExceedTasksCnt)
}

func (w *Worker) reportEnabledTimersCnt(ctx context.Context) {
	enabledTimerCnt, err := w.timerDAO.Count(ctx, timer_dao.WithStatus(int32(consts.Enable)))
	if err != nil {
		log.ErrorContextf(ctx, "[monitor] get enabled timer cnt failed, err: %v", err)
		return
	}
	w.reporter.ReportTimerEnabledRecord(float64(enabledTimerCnt))
	log.InfoContextf(ctx, "[monitor] report enabled timer cnt success, cnt: %d", enabledTimerCnt)
}
