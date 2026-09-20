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

package scheduler

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/conf"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/utils"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/log"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/pool"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/redis"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/service/trigger"
)

type Worker struct {
	pool            pool.WorkerPool
	appConfProvider appConfProvider
	trigger         *trigger.Worker
	lockService     lockService
	bucketGetter    bucketGetter
	minuteBuckets   map[string]int
}

func NewWorker(trigger *trigger.Worker, redisClient *redis.Client, appConfProvider *conf.SchedulerAppConfProvider) *Worker {
	return &Worker{
		pool:            pool.NewGoWorkerPool(appConfProvider.Get().WorkersNum),
		trigger:         trigger,
		lockService:     redisClient,
		bucketGetter:    redisClient,
		appConfProvider: appConfProvider,
		minuteBuckets:   make(map[string]int),
	}
}

func (w *Worker) Start(ctx context.Context) error {
	w.trigger.Start(ctx)

	ticker := time.NewTicker(time.Duration(w.appConfProvider.Get().TryLockGapMilliSeconds) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.WarnContext(ctx, "stopped")
			return nil
		case <-ticker.C:
		}

		w.handleSlices(ctx)
	}
}

func (w *Worker) handleSlices(ctx context.Context) {
	for i := 0; i < w.getValidBucket(ctx); i++ {
		w.handleSlice(ctx, i)
	}
}

// Dynamic bucketing is disabled.
func (w *Worker) getValidBucket(ctx context.Context) int {
	return w.appConfProvider.Get().BucketsNum
	// now := time.Now()
	// // Delete data from the previous minute
	// delete(w.minuteBuckets, now.Add(-time.Minute).Format(consts.MinuteFormat))

	// // Reuse data within the same minute
	// bucket, ok := w.minuteBuckets[now.Format(consts.MinuteFormat)]
	// if ok {
	// 	return bucket
	// }

	// // Store in map for reuse
	// defer func() {
	// 	w.minuteBuckets[now.Format(consts.MinuteFormat)] = bucket
	// }()

	// bucket = w.appConfProvider.Get().BucketsNum
	// bucketKey := utils.GetBucketCntKey(now.Format(consts.MinuteFormat))
	// res, err := w.bucketGetter.Get(ctx, bucketKey)
	// if err != nil {
	// 	log.ErrorContextf(ctx, "[scheduler] get bucket failed, key: %s, err:%v", bucketKey, err)
	// 	return bucket
	// }

	// _bucket, err := strconv.Atoi(res)
	// if err != nil {
	// 	log.ErrorContextf(ctx, "[scheduler] get invalid bucket, key: %s, got:%v", bucketKey, res)
	// 	return bucket
	// }

	// bucket = _bucket
	// log.InfoContextf(ctx, "[scheduler] get valid bucket success, bucket: %d, cur: %v", _bucket, time.Now())

	// return bucket
}

func (w *Worker) handleSlice(ctx context.Context, bucketID int) {
	// log.InfoContextf(ctx, "scheduler_1 start: %v", time.Now())
	now := time.Now()
	if err := w.pool.Submit(func() {
		w.asyncHandleSlice(ctx, now.Add(-time.Minute), bucketID)
	}); err != nil {
		log.ErrorContextf(ctx, "[handle slice] submit task failed, err: %v", err)
	}
	if err := w.pool.Submit(func() {
		w.asyncHandleSlice(ctx, now, bucketID)
	}); err != nil {
		log.ErrorContextf(ctx, "[handle slice] submit task failed, err: %v", err)
	}
	// log.InfoContextf(ctx, "scheduler_1 end: %v", time.Now())
}

func (w *Worker) asyncHandleSlice(ctx context.Context, t time.Time, bucketID int) {
	// log.InfoContextf(ctx, "scheduler_2 start: %v", time.Now())
	// defer func() {
	// 	log.InfoContextf(ctx, "scheduler_2 end: %v", time.Now())
	// }()

	locker := w.lockService.GetDistributionLock(utils.GetTimeBucketLockKey(t, bucketID))
	if err := locker.Lock(ctx, int64(w.appConfProvider.Get().TryLockSeconds)); err != nil {
		// log.WarnContextf(ctx, "get lock failed, err: %v, key: %s", err, utils.GetTimeBucketLockKey(t, bucketID))
		return
	}

	log.InfoContextf(ctx, "get scheduler lock success, key: %s", utils.GetTimeBucketLockKey(t, bucketID))

	ack := func() {
		if err := locker.ExpireLock(ctx, int64(w.appConfProvider.Get().SuccessExpireSeconds)); err != nil {
			log.ErrorContextf(ctx, "expire lock failed, lock key: %s, err: %v", utils.GetTimeBucketLockKey(t, bucketID), err)
		}
	}

	if err := w.trigger.Work(ctx, utils.GetSliceMsgKey(t, bucketID), ack); err != nil {
		log.ErrorContextf(ctx, "trigger work failed, err: %v", err)
	}
}

type appConfProvider interface {
	Get() *conf.SchedulerAppConf
}

type lockService interface {
	GetDistributionLock(key string) redis.DistributeLocker
}

type bucketGetter interface {
	Get(ctx context.Context, key string) (string, error)
}
