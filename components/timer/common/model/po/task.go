package po

import (
	"time"

	"gorm.io/gorm"
)

// Task is an execution record for a timer run.
type Task struct {
	gorm.Model
	App      string    `gorm:"column:app;NOT NULL"`           // App name
	TimerID  uint      `gorm:"column:timer_id;NOT NULL"`      // Timer ID
	Output   string    `gorm:"column:output;default:null"`    // Execution result
	RunTimer time.Time `gorm:"column:run_timer;default:null"` // Execution time
	CostTime int       `gorm:"column:cost_time"`              // Execution cost in milliseconds
	Status   int       `gorm:"column:status;NOT NULL"`        // Current status
}

func (t *Task) TableName() string {
	return "task"
}

type MinuteTaskCnt struct {
	Minute string `gorm:"column:minute"`
	Cnt    int64  `gorm:"column:cnt"`
}
