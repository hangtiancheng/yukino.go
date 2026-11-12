package task

import (
	"context"
	"fmt"
	"time"

	"github.com/hangtiancheng/yukino.go/components/timer/common/conf"
	"github.com/hangtiancheng/yukino.go/components/timer/common/consts"
	"github.com/hangtiancheng/yukino.go/components/timer/common/model/po"
	"github.com/hangtiancheng/yukino.go/components/timer/common/utils"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/redis"
)

type TaskCache struct {
	client       cacheClient
	confProvider *conf.SchedulerAppConfProvider
}

func NewTaskCache(client *redis.Client, confProvider *conf.SchedulerAppConfProvider) *TaskCache {
	return &TaskCache{client: client, confProvider: confProvider}
}

func (t *TaskCache) BatchCreateBucket(ctx context.Context, cntByMins []*po.MinuteTaskCnt, end time.Time) error {
	if t == nil || t.client == nil || t.confProvider == nil || t.confProvider.Get() == nil || ctx == nil {
		return fmt.Errorf("task cache dependencies and context are required")
	}
	conf := t.confProvider.Get()
	if conf.BucketsNum <= 0 {
		return fmt.Errorf("bucket count must be positive")
	}

	remaining := time.Until(end)
	if remaining <= 0 {
		return fmt.Errorf("bucket expiration must be in the future")
	}
	expireSeconds := int64((remaining-1)/time.Second + 1)
	commands := make([]*redis.Command, 0, 2*len(cntByMins))
	for _, detail := range cntByMins {
		if detail == nil || detail.Cnt < 0 {
			return fmt.Errorf("invalid minute task count")
		}
		increment := detail.Cnt / 200
		base := int64(conf.BucketsNum)
		if increment > int64(^uint64(0)>>1)-base {
			return fmt.Errorf("bucket count overflows for minute %q", detail.Minute)
		}
		bucketCount := base + increment
		commands = append(commands, redis.NewSetCommand(utils.GetBucketCntKey(detail.Minute), bucketCount))
		commands = append(commands, redis.NewExpireCommand(utils.GetBucketCntKey(detail.Minute), expireSeconds))
	}

	_, err := t.client.Transaction(ctx, commands...)
	return err
}

func (t *TaskCache) BatchCreateTasks(ctx context.Context, tasks []*po.Task, start, end time.Time) error {
	if t == nil || t.client == nil || t.confProvider == nil || t.confProvider.Get() == nil || ctx == nil {
		return fmt.Errorf("task cache dependencies and context are required")
	}
	if len(tasks) == 0 {
		return nil
	}
	if t.confProvider.Get().BucketsNum <= 0 {
		return fmt.Errorf("bucket count must be positive")
	}

	commands := make([]*redis.Command, 0, 2*len(tasks))
	for _, task := range tasks {
		if task == nil {
			return fmt.Errorf("nil task")
		}
		unix := task.RunTimer.UnixMilli()
		tableName := t.GetTableName(task)
		commands = append(commands, redis.NewZAddCommand(tableName, unix, utils.UnionTimerIDUnix(task.TimerID, unix)))
		remaining := time.Until(task.RunTimer.Add(24 * time.Hour))
		if remaining <= 0 {
			return fmt.Errorf("task %d cache retention has expired", task.ID)
		}
		aliveSeconds := int64((remaining-1)/time.Second + 1)
		commands = append(commands, redis.NewExpireCommand(tableName, aliveSeconds))
	}

	_, err := t.client.Transaction(ctx, commands...)
	return err
}

func (t *TaskCache) GetTasksByTime(ctx context.Context, table string, start, end int64) ([]*po.Task, error) {
	if t == nil || t.client == nil || ctx == nil || table == "" {
		return nil, fmt.Errorf("task cache client, context, and table are required")
	}
	if end <= start {
		return []*po.Task{}, nil
	}
	timerIDUnix, err := t.client.ZrangeByScore(ctx, table, start, end-1)
	if err != nil {
		return nil, err
	}

	tasks := make([]*po.Task, 0, len(timerIDUnix))
	for _, timerIDUnix := range timerIDUnix {
		timerID, unix, err := utils.SplitTimerIDUnix(timerIDUnix)
		if err != nil {
			return nil, fmt.Errorf("invalid cached timer id %q: %w", timerIDUnix, err)
		}
		tasks = append(tasks, &po.Task{
			TimerID:  timerID,
			RunTimer: time.UnixMilli(unix),
		})
	}

	return tasks, nil
}

func (t *TaskCache) GetTableName(task *po.Task) string {
	if t == nil || task == nil || t.confProvider == nil || t.confProvider.Get() == nil {
		return ""
	}
	maxBucket := t.confProvider.Get().BucketsNum
	if maxBucket <= 0 {
		return ""
	}

	return fmt.Sprintf("%s_%d", task.RunTimer.Format(consts.MinuteFormat), task.TimerID%uint(maxBucket))
}

type cacheClient interface {
	Transaction(ctx context.Context, commands ...*redis.Command) ([]any, error)
	ZrangeByScore(ctx context.Context, table string, score1, score2 int64) ([]string, error)
	Expire(ctx context.Context, key string, expireSeconds int64) error
	MGet(ctx context.Context, keys ...string) ([]string, error)
}
