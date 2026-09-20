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

package webserver

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/consts"
	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/vo"
	dao "github.com/hangtiancheng/yukino.go/apps/timer_demo/dao/task"
)

type TaskService struct {
	dao *dao.TaskDAO
}

func NewTaskService(dao *dao.TaskDAO) *TaskService {
	return &TaskService{dao: dao}
}

func (t *TaskService) GetTask(ctx context.Context, id uint) (*vo.Task, error) {
	task, err := t.dao.GetTask(ctx, dao.WithTaskID(id))
	if err != nil {
		return nil, err
	}
	return vo.NewTask(task), nil
}

func (t *TaskService) GetTasks(ctx context.Context, req *vo.GetTasksReq) ([]*vo.Task, int64, error) {
	total, err := t.dao.Count(ctx, dao.WithTimerID(req.TimerID), dao.WithStatuses([]int32{
		int32(consts.Running),
		int32(consts.Succeed),
		int32(consts.Failed),
	}))
	if err != nil {
		return nil, -1, err
	}

	offset, limit := req.Get()
	if total <= int64(offset) {
		return []*vo.Task{}, total, nil
	}
	tasks, err := t.dao.GetTasks(ctx, dao.WithTimerID(req.TimerID), dao.WithPageLimit(offset, limit), dao.WithStatuses([]int32{
		int32(consts.Running),
		int32(consts.Succeed),
		int32(consts.Failed),
	}), dao.WithDesc())
	if err != nil {
		return nil, -1, err
	}

	return vo.NewTasks(tasks), total, nil
}
