package trigger

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/components/timer/common/conf"
	"github.com/hangtiancheng/yukino.go/components/timer/common/model/vo"
	"github.com/hangtiancheng/yukino.go/components/timer/common/utils"
	"github.com/hangtiancheng/yukino.go/components/timer/internal/service/executor"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/concurrency"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/log"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/pool"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/redis"
)

type Worker struct {
	task         taskService
	confProvider confProvider
	pool         pool.WorkerPool
	executor     *executor.Worker
	lockService  *redis.Client
}

func NewWorker(executor *executor.Worker, task *TaskService, lockService *redis.Client, confProvider *conf.TriggerAppConfProvider) *Worker {
	return &Worker{
		executor:     executor,
		task:         task,
		lockService:  lockService,
		pool:         pool.NewGoWorkerPool(confProvider.Get().WorkersNum),
		confProvider: confProvider,
	}
}

func (w *Worker) Start(ctx context.Context) {
	if ctx != nil && w.executor != nil {
		w.executor.Start(ctx)
	}
}

func (w *Worker) Work(ctx context.Context, minuteBucketKey string, ack func()) error {
	if ctx == nil || ack == nil {
		return fmt.Errorf("context and ack callback are required")
	}
	if w.confProvider == nil || w.confProvider.Get() == nil || w.task == nil || w.executor == nil || w.pool == nil {
		return fmt.Errorf("trigger worker dependencies are incomplete")
	}

	startTime, err := getStartMinute(minuteBucketKey)
	if err != nil {
		return err
	}

	conf := w.confProvider.Get()
	gap := time.Duration(conf.ZRangeGapSeconds) * time.Second
	if gap <= 0 {
		return fmt.Errorf("zrange gap must be positive")
	}
	ticker := time.NewTicker(gap)
	defer ticker.Stop()

	endTime := startTime.Add(time.Minute)
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	notifier := concurrency.NewSafeChan(int(time.Minute/gap) + 2)
	defer notifier.Close()

	var wg sync.WaitGroup
	wg.Go(func() {
		if err := w.handleBatch(workCtx, minuteBucketKey, startTime, minTime(startTime.Add(gap), endTime)); err != nil {
			notifier.Put(err)
		}
	})

loop:
	for {
		select {
		case <-ctx.Done():
			cancel()
			wg.Wait()
			return ctx.Err()
		case e := <-notifier.GetChan():
			if batchErr, ok := e.(error); ok {
				cancel()
				wg.Wait()
				return batchErr
			}
		case <-ticker.C:
		}

		if startTime = startTime.Add(gap); !startTime.Before(endTime) {
			break loop
		}

		batchStart := startTime
		batchEnd := minTime(batchStart.Add(gap), endTime)
		wg.Go(func() {
			if err := w.handleBatch(workCtx, minuteBucketKey, batchStart, batchEnd); err != nil {
				notifier.Put(err)
			}
		})
	}

	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case e := <-notifier.GetChan():
		err, _ = e.(error)
		return err
	default:
	}

	ack()
	log.InfoContextf(ctx, "ack success, key: %s", minuteBucketKey)
	return nil
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func (w *Worker) handleBatch(ctx context.Context, key string, start, end time.Time) error {
	bucket, err := getBucket(key)
	if err != nil {
		return err
	}

	tasks, err := w.task.GetTasksByTime(ctx, key, bucket, start, end)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		if err := w.pool.Submit(func() {
			if err := w.executor.Work(ctx, utils.UnionTimerIDUnix(task.TimerID, task.RunTimer.UnixMilli())); err != nil {
				log.ErrorContextf(ctx, "executor work failed, err: %v", err)
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

func getStartMinute(slice string) (time.Time, error) {
	timeBucket := strings.Split(slice, "_")
	if len(timeBucket) != 2 {
		return time.Time{}, fmt.Errorf("invalid format of msg key: %s", slice)
	}

	return utils.GetStartMinute(timeBucket[0])
}

func getBucket(slice string) (int, error) {
	timeBucket := strings.Split(slice, "_")
	if len(timeBucket) != 2 {
		return -1, fmt.Errorf("invalid format of msg key: %s", slice)
	}
	return strconv.Atoi(timeBucket[1])
}

type taskService interface {
	GetTasksByTime(ctx context.Context, key string, bucket int, start, end time.Time) ([]*vo.Task, error)
}

type confProvider interface {
	Get() *conf.TriggerAppConf
}
