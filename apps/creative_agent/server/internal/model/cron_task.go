package model

// CGCronTask is the GORM model of the cg_cron_task table.
//
// Multi-agent auto-advance index for cron-task sessions (05-subagent.md §5.5 path B).
// The Poller scans Running records every 30s; after the current task_run is Finished it increments CurrentStepIndex and dispatches the next agent.
type CGCronTask struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// Cron-task business primary key (snowflake ID integer)
	CronTaskID int64 `gorm:"column:cron_task_id;uniqueIndex;not null"`
	// Session ID
	SessionID string `gorm:"column:session_id;index;size:64;not null"`
	// Associated manager message_id in cg_task_run
	MsgID string `gorm:"column:msg_id;index;size:64;not null;default:''"`
	// Full agents-plan JSON (isomorphic to cg_task_run.plan_steps)
	PlanSteps string `gorm:"column:plan_steps;type:mediumtext;not null"`
	// Current step index (0-based, points to PlanSteps.Agents[index])
	CurrentStepIndex int32 `gorm:"column:current_step_index;not null;default:0"`
	// Status: 0=Running 1=Done 2=Failed
	Status int32 `gorm:"column:status;not null;default:0"`
	// Failure reason
	Reason string `gorm:"column:reason;type:text;not null"`
	// Product line
	Pd string `gorm:"column:pd;size:32;not null;default:''"`
	// Creation time (millisecond timestamp)
	CreateTime int64 `gorm:"column:create_time;not null"`
	// Update time (millisecond timestamp)
	UpdateTime int64 `gorm:"column:update_time;not null"`
}

func (CGCronTask) TableName() string { return "cg_cron_task" }

// cg_cron_task status constants.
const (
	CronTaskStatusRunning int32 = 0 // running (the Poller will advance it)
	CronTaskStatusDone    int32 = 1 // all agents executed
	CronTaskStatusFailed  int32 = 2 // advancement failed
)
