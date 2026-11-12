package executor

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/components/timer/common/conf"
	"github.com/hangtiancheng/yukino.go/components/timer/common/consts"
	"github.com/hangtiancheng/yukino.go/components/timer/common/model/po"
	"github.com/hangtiancheng/yukino.go/components/timer/common/model/vo"
	task_dao "github.com/hangtiancheng/yukino.go/components/timer/internal/dao/task"
	timer_dao "github.com/hangtiancheng/yukino.go/components/timer/internal/dao/timer"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/log"
)

type TimerService struct {
	sync.Once
	lifecycleMu  sync.Mutex
	confProvider *conf.MigratorAppConfProvider
	ctx          context.Context
	stop         func()
	done         chan struct{}
	mu           sync.RWMutex
	timers       map[uint]*vo.Timer
	timerDAO     timerDAO
	taskDAO      *task_dao.TaskDAO
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
	if t == nil || ctx == nil || t.confProvider == nil || t.confProvider.Get() == nil {
		log.Errorf("timer service requires context and configuration")
		return
	}
	stepMinutes := t.confProvider.Get().TimerDetailCacheMinutes
	if stepMinutes <= 0 {
		log.Errorf("timer detail cache interval must be positive")
		return
	}
	t.lifecycleMu.Lock()
	defer t.lifecycleMu.Unlock()
	t.Do(func() {
		t.ctx, t.stop = context.WithCancel(ctx)
		t.done = make(chan struct{})

		go func() {
			defer close(t.done)
			ticker := time.NewTicker(time.Duration(stepMinutes) * time.Minute)
			defer ticker.Stop()

			for {
				select {
				case <-t.ctx.Done():
					return
				case <-ticker.C:
				}

				t.refreshTimers(t.ctx, stepMinutes)
			}
		}()
	})
}

func (t *TimerService) refreshTimers(ctx context.Context, stepMinutes int) {
	start := time.Now()
	timers, err := t.getTimersByTime(ctx, start, start.Add(time.Duration(stepMinutes)*time.Minute))
	if err != nil {
		log.ErrorContextf(ctx, "refresh timers failed, start: %v, err: %v", start, err)
		return
	}
	if timers == nil {
		timers = make(map[uint]*vo.Timer)
	}

	t.mu.Lock()
	t.timers = timers
	t.mu.Unlock()
}

func (t *TimerService) getTimersByTime(ctx context.Context, start, end time.Time) (map[uint]*vo.Timer, error) {
	if t == nil || t.taskDAO == nil || t.timerDAO == nil || ctx == nil {
		return nil, errors.New("timer service dependencies and context are required")
	}
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
		if task == nil {
			continue
		}
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
	if t == nil || ctx == nil || t.timerDAO == nil {
		return nil, errors.New("timer service, context, and timer dao are required")
	}
	t.mu.RLock()
	vTimer, ok := t.timers[id]
	t.mu.RUnlock()
	if ok {
		return cloneTimer(vTimer), nil
	}

	log.WarnContextf(ctx, "get timer from local cache failed, timerID: %d", id)

	timer, err := t.timerDAO.GetTimer(ctx, timer_dao.WithID(id))
	if err != nil {
		return nil, err
	}

	return vo.NewTimer(timer)
}

func (t *TimerService) Stop() {
	if t == nil {
		return
	}
	t.lifecycleMu.Lock()
	stop, done := t.stop, t.done
	t.lifecycleMu.Unlock()
	if stop != nil {
		stop()
	}
	if done != nil {
		<-done
	}
}

func cloneTimer(timer *vo.Timer) *vo.Timer {
	if timer == nil {
		return nil
	}
	cloned := *timer
	if timer.NotifyHTTPParam != nil {
		params := *timer.NotifyHTTPParam
		if timer.NotifyHTTPParam.Header != nil {
			params.Header = make(map[string]string, len(timer.NotifyHTTPParam.Header))
			for key, value := range timer.NotifyHTTPParam.Header {
				params.Header[key] = value
			}
		}
		cloned.NotifyHTTPParam = &params
	}
	return &cloned
}

type timerDAO interface {
	GetTimer(context.Context, ...timer_dao.Option) (*po.Timer, error)
	GetTimers(ctx context.Context, opts ...timer_dao.Option) ([]*po.Timer, error)
}
