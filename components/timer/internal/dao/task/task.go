package task

import (
	"context"
	"errors"

	"github.com/hangtiancheng/yukino.go/components/timer/common/model/po"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/mysql"
)

type TaskDAO struct {
	client *mysql.Client
}

func NewTaskDAO(client *mysql.Client) *TaskDAO {
	return &TaskDAO{
		client: client,
	}
}

func (t *TaskDAO) GetTask(ctx context.Context, opts ...Option) (*po.Task, error) {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil {
		return nil, errors.New("task dao and context are required")
	}
	db := t.client.WithContext(ctx)
	for _, opt := range opts {
		if opt != nil {
			db = opt(db)
		}
	}

	var task po.Task
	return &task, db.First(&task).Error
}

func (t *TaskDAO) GetTasks(ctx context.Context, opts ...Option) ([]*po.Task, error) {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil {
		return nil, errors.New("task dao and context are required")
	}
	db := t.client.WithContext(ctx)
	for _, opt := range opts {
		if opt != nil {
			db = opt(db)
		}
	}

	var tasks []*po.Task
	return tasks, db.Model(&po.Task{}).Scan(&tasks).Error
}

func (t *TaskDAO) UpdateTask(ctx context.Context, task *po.Task) error {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil || task == nil {
		return errors.New("task dao, context, and task are required")
	}
	return t.client.DB.WithContext(ctx).Updates(task).Error
}

func (t *TaskDAO) Count(ctx context.Context, opts ...Option) (int64, error) {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil {
		return 0, errors.New("task dao and context are required")
	}
	db := t.client.DB.WithContext(ctx).Model(&po.Task{})
	for _, opt := range opts {
		if opt != nil {
			db = opt(db)
		}
	}
	var cnt int64
	return cnt, db.Count(&cnt).Error
}

func (t *TaskDAO) CountGroupByMinute(ctx context.Context, startTimeStr, endTimeStr string) ([]*po.MinuteTaskCnt, error) {
	if t == nil || t.client == nil || t.client.DB == nil || ctx == nil {
		return nil, errors.New("task dao and context are required")
	}

	var res []*po.MinuteTaskCnt
	return res, t.client.DB.WithContext(ctx).Raw(SQLGetMinuteTaskCnt, startTimeStr, endTimeStr).Scan(&res).Error
}
