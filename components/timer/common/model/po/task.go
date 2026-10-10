package po

import (
	"time"

	"gorm.io/gorm"
)

type Task struct {
	gorm.Model
	App      string    `gorm:"column:app;NOT NULL"`
	TimerID  uint      `gorm:"column:timer_id;NOT NULL"`
	Output   string    `gorm:"column:output;default:null"`
	RunTimer time.Time `gorm:"column:run_timer;default:null"`
	CostTime int       `gorm:"column:cost_time"`
	Status   int       `gorm:"column:status;NOT NULL"`
}

func (t *Task) TableName() string {
	return "task"
}

type MinuteTaskCnt struct {
	Minute string `gorm:"column:minute"`
	Cnt    int64  `gorm:"column:cnt"`
}
