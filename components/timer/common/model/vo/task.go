package vo

import (
	"time"

	"github.com/hangtiancheng/yukino.go/components/timer/common/model/po"
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

type Task struct {
	ID       uint      `json:"id"`
	App      string    `json:"app"`
	TimerID  uint      `json:"timerID"`
	Output   string    `json:"output"`
	RunTimer time.Time `json:"runTimer"`
	CostTime int       `json:"costTime"`
	Status   int       `json:"status"`
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
