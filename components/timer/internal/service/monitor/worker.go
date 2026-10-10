package monitor

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/components/timer/common/consts"
	"github.com/hangtiancheng/yukino.go/components/timer/common/utils"
	task_dao "github.com/hangtiancheng/yukino.go/components/timer/internal/dao/task"
	timer_dao "github.com/hangtiancheng/yukino.go/components/timer/internal/dao/timer"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/log"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/promethus"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/redis"
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
