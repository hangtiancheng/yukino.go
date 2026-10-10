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
	conf := t.confProvider.Get()

	expireSeconds := int64(time.Until(end) / time.Second)
	commands := make([]*redis.Command, 0, 2*len(cntByMins))
	for _, detail := range cntByMins {
		commands = append(commands, redis.NewSetCommand(utils.GetBucketCntKey(detail.Minute), conf.BucketsNum+int(detail.Cnt)/200))
		commands = append(commands, redis.NewExpireCommand(utils.GetBucketCntKey(detail.Minute), expireSeconds))
	}

	_, err := t.client.Transaction(ctx, commands...)
	return err
}

func (t *TaskCache) BatchCreateTasks(ctx context.Context, tasks []*po.Task, start, end time.Time) error {
	if len(tasks) == 0 {
		return nil
	}

	commands := make([]*redis.Command, 0, 2*len(tasks))
	for _, task := range tasks {
		unix := task.RunTimer.UnixMilli()
		tableName := t.GetTableName(task)
		commands = append(commands, redis.NewZAddCommand(tableName, unix, utils.UnionTimerIDUnix(task.TimerID, unix)))
		aliveSeconds := int64(time.Until(task.RunTimer.Add(24*time.Hour)) / time.Second)
		commands = append(commands, redis.NewExpireCommand(tableName, aliveSeconds))
	}

	_, err := t.client.Transaction(ctx, commands...)
	return err
}

func (t *TaskCache) GetTasksByTime(ctx context.Context, table string, start, end int64) ([]*po.Task, error) {
	timerIDUnix, err := t.client.ZrangeByScore(ctx, table, start, end-1)
	if err != nil {
		return nil, err
	}

	tasks := make([]*po.Task, 0, len(timerIDUnix))
	for _, timerIDUnix := range timerIDUnix {
		timerID, unix, _ := utils.SplitTimerIDUnix(timerIDUnix)
		tasks = append(tasks, &po.Task{
			TimerID:  timerID,
			RunTimer: time.UnixMilli(unix),
		})
	}

	return tasks, nil
}

func (t *TaskCache) GetTableName(task *po.Task) string {
	maxBucket := t.confProvider.Get().BucketsNum

	return fmt.Sprintf("%s_%d", task.RunTimer.Format(consts.MinuteFormat), int64(task.TimerID)%int64(maxBucket))
}

type cacheClient interface {
	Transaction(ctx context.Context, commands ...*redis.Command) ([]any, error)
	ZrangeByScore(ctx context.Context, table string, score1, score2 int64) ([]string, error)
	Expire(ctx context.Context, key string, expireSeconds int64) error
	MGet(ctx context.Context, keys ...string) ([]string, error)
}
