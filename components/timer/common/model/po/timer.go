package po

import (
	"time"

	"github.com/hangtiancheng/yukino.go/components/timer/common/consts"

	"gorm.io/gorm"
)

type Timer struct {
	gorm.Model
	App             string `gorm:"column:app;NOT NULL" json:"app,omitempty"`
	Name            string `gorm:"column:name;NOT NULL" json:"name,omitempty"`
	Status          int    `gorm:"column:status;NOT NULL" json:"status,omitempty"`
	Cron            string `gorm:"column:cron;NOT NULL" json:"cron,omitempty"`
	NotifyHTTPParam string `gorm:"column:notify_http_param;NOT NULL" json:"notify_http_param,omitempty"`
}

func (t *Timer) TableName() string {
	return "timer"
}

func (t *Timer) BatchTasksFromTimer(executeTimes []time.Time) []*Task {
	tasks := make([]*Task, 0, len(executeTimes))
	for _, executeTime := range executeTimes {
		tasks = append(tasks, &Task{
			App:      t.App,
			TimerID:  t.ID,
			Status:   consts.NotRun.ToInt(),
			RunTimer: executeTime,
		})
	}
	return tasks
}
