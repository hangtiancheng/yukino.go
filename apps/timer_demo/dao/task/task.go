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

package task

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/po"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/pkg/mysql"
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
	db := t.client.WithContext(ctx)
	for _, opt := range opts {
		db = opt(db)
	}

	var task po.Task
	return &task, db.First(&task).Error
}

func (t *TaskDAO) GetTasks(ctx context.Context, opts ...Option) ([]*po.Task, error) {
	db := t.client.WithContext(ctx)
	for _, opt := range opts {
		db = opt(db)
	}

	var tasks []*po.Task
	return tasks, db.Model(&po.Task{}).Scan(&tasks).Error
}

func (t *TaskDAO) UpdateTask(ctx context.Context, task *po.Task) error {
	return t.client.DB.WithContext(ctx).Updates(task).Error
}

func (t *TaskDAO) Count(ctx context.Context, opts ...Option) (int64, error) {
	db := t.client.DB.WithContext(ctx).Model(&po.Task{})
	for _, opt := range opts {
		db = opt(db)
	}
	var cnt int64
	return cnt, db.Count(&cnt).Error
}

func (t *TaskDAO) CountGroupByMinute(ctx context.Context, startTimeStr, endTimeStr string) ([]*po.MinuteTaskCnt, error) {
	_sql := fmt.Sprintf(SQLGetMinuteTaskCnt, startTimeStr, endTimeStr)

	var res []*po.MinuteTaskCnt
	return res, t.client.DB.Raw(_sql).Scan(&res).Error
}
