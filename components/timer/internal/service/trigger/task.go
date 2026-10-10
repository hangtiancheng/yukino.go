package trigger

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/components/timer/common/conf"
	"github.com/hangtiancheng/yukino.go/components/timer/common/consts"
	"github.com/hangtiancheng/yukino.go/components/timer/common/model/po"
	"github.com/hangtiancheng/yukino.go/components/timer/common/model/vo"
	dao "github.com/hangtiancheng/yukino.go/components/timer/internal/dao/task"
)

type TaskService struct {
	confProvider *conf.SchedulerAppConfProvider
	cache        *dao.TaskCache
	dao          taskDAO
}

func NewTaskService(dao *dao.TaskDAO, cache *dao.TaskCache, confProvider *conf.SchedulerAppConfProvider) *TaskService {
	return &TaskService{
		confProvider: confProvider,
		dao:          dao,
		cache:        cache,
	}
}

func (t *TaskService) GetTasksByTime(ctx context.Context, key string, bucket int, start, end time.Time) ([]*vo.Task, error) {
	if tasks, err := t.cache.GetTasksByTime(ctx, key, start.UnixMilli(), end.UnixMilli()); err == nil && len(tasks) > 0 {
		return vo.NewTasks(tasks), nil
	}

	tasks, err := t.dao.GetTasks(ctx, dao.WithStartTime(start), dao.WithEndTime(end), dao.WithStatus(int32(consts.NotRun.ToInt())))
	if err != nil {
		return nil, err
	}

	maxBucket := t.confProvider.Get().BucketsNum
	var validTask []*po.Task
	for _, task := range tasks {
		if task.TimerID%uint(maxBucket) != uint(bucket) {
			continue
		}
		validTask = append(validTask, task)
	}

	return vo.NewTasks(validTask), nil
}

type taskDAO interface {
	GetTasks(ctx context.Context, opts ...dao.Option) ([]*po.Task, error)
}
