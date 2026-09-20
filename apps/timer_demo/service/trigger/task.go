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

package trigger

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/conf"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/consts"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/po"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/vo"
	dao "github.com/hangtiancheng/yukino.go/apps/timer_demo/dao/task"
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
	// Try cache first
	if tasks, err := t.cache.GetTasksByTime(ctx, key, start.UnixMilli(), end.UnixMilli()); err == nil && len(tasks) > 0 {
		return vo.NewTasks(tasks), nil
	}

	// Fall back to database on cache miss
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
