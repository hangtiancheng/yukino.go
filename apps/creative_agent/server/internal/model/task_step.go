package model

// CGTaskStep is the GORM model of the cg_task_step table.
//
// Responsibility: one record per sub-agent execution step. Supports a tree structure (parent_step_id self-reference);
// a single sub-agent invocation can have multiple sub-steps (tool calls).
// step_output is the sole data source for the conversation Worker's backflow to the frontend — the poll loop reads this column and pushes SSE.
type CGTaskStep struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// Step business primary key (snowflake ID integer), globally unique
	TaskStepID int64 `gorm:"column:task_step_id;uniqueIndex;not null"`
	// Associated with cg_task_run.task_run_id
	TaskRunID int64 `gorm:"column:task_run_id;index;not null"`
	// Parent step ID (0 = top-level step, i.e. sub-agent level)
	ParentStepID int64 `gorm:"column:parent_step_id;index;not null;default:0"`
	// User ID
	UserID int64 `gorm:"column:user_id;not null"`
	// Agent ID
	AgentID string `gorm:"column:agent_id;size:64;not null"`
	// Session ID
	SessionID string `gorm:"column:session_id;index;size:64;not null"`
	// Cron task ID (0 when not cron-driven)
	CronTaskID int64 `gorm:"column:cron_task_id;not null;default:0"`
	// Step name (sub-agent level = agent name, tool-call level = tool name)
	StepName string `gorm:"column:step_name;size:256;not null;default:''"`
	// Step input JSON
	StepInput string `gorm:"column:step_input;type:mediumtext;not null"`
	// Step output JSON (read by the poll backflow)
	StepOutput string `gorm:"column:step_output;type:mediumtext;not null"`
	// Intermediate result JSON (interim product when need_wait=Yes)
	IntermediateResult string `gorm:"column:intermediate_result;type:mediumtext;not null"`
	// Status: 0=Pending, 1=Running, 2=Interrupted, 3=Finished, 4=Failed, 5=Skipped
	Status int32 `gorm:"column:status;not null;default:0"`
	// Whether waiting is needed: 0=No 1=Yes (an async pipeline was triggered; waiting for its callback)
	NeedWait int8 `gorm:"column:need_wait;not null;default:0"`
	// Product line
	Pd string `gorm:"column:pd;size:32;not null;default:''"`
	// Agent type
	AgentType string `gorm:"column:agent_type;size:32;not null;default:''"`
	// Start time (millisecond timestamp)
	StartTime int64 `gorm:"column:start_time;not null;default:0"`
	// End time (millisecond timestamp)
	EndTime int64 `gorm:"column:end_time;not null;default:0"`
	// Shard key (fixed to 0 in the replica environment)
	ShardingKey int32 `gorm:"column:sharding_key;not null;default:0"`
	// Creation time (millisecond timestamp)
	CreateTime int64 `gorm:"column:create_time;not null"`
	// Update time (millisecond timestamp)
	UpdateTime int64 `gorm:"column:update_time;not null"`
}

func (CGTaskStep) TableName() string { return "cg_task_step" }

// cg_task_step status constants (the reference system has 6 statuses).
const (
	TaskStepStatusPending     int32 = 0 // waiting to execute
	TaskStepStatusRunning     int32 = 1 // running
	TaskStepStatusInterrupted int32 = 2 // interrupted (HIL breakpoint awaiting user action)
	TaskStepStatusFinished    int32 = 3 // finished
	TaskStepStatusFailed      int32 = 4 // execution failed
	TaskStepStatusSkipped     int32 = 5 // skipped
)

// NeedWait constants.
const (
	NeedWaitNo  int8 = 0 // the sub-agent produced the terminal result itself
	NeedWaitYes int8 = 1 // an async pipeline was triggered; waiting for its callback
)
