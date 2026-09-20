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

package vo

import (
	"time"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/po"
)

type GetTasksReq struct {
	PageLimiter
	TimerID uint `form:"timerID" binding:"required"`
}

type GetTasksResp struct {
	CodeMsg
	Total int64   `json:"total"`
	Data  []*Task `json:"data"`
}

func NewGetTasksResp(tasks []*Task, total int64, codeMsg CodeMsg) *GetTasksResp {
	return &GetTasksResp{
		CodeMsg: codeMsg,
		Total:   total,
		Data:    tasks,
	}
}

// Task is an execution record for a timer run.
type Task struct {
	ID       uint      `json:"id"`       // Task ID
	App      string    `json:"app"`      // App name
	TimerID  uint      `json:"timerID"`  // Timer ID
	Output   string    `json:"output"`   // Execution result
	RunTimer time.Time `json:"runTimer"` // Execution time
	CostTime int       `json:"costTime"` // Execution cost
	Status   int       `json:"status"`   // Current status
}

func NewTask(task *po.Task) *Task {
	return &Task{
		ID:       task.ID,
		App:      task.App,
		TimerID:  task.TimerID,
		Output:   task.Output,
		RunTimer: task.RunTimer,
		CostTime: task.CostTime,
		Status:   task.Status,
	}
}

func NewTasks(tasks []*po.Task) []*Task {
	vTasks := make([]*Task, 0, len(tasks))
	for _, task := range tasks {
		vTasks = append(vTasks, NewTask(task))
	}
	return vTasks
}

func (t *Task) ToPO() *po.Task {
	return &po.Task{
		App:      t.App,
		TimerID:  t.TimerID,
		Output:   t.Output,
		RunTimer: t.RunTimer,
		CostTime: t.CostTime,
		Status:   t.Status,
	}
}
