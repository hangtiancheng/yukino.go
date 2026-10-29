package model

// CGAgentThought is the GORM model of the cg_agent_thought table.
// It records the input/output trace of every manager/sub-agent LLM round (aligned with the reference system's cg_agent_thought),
// for debugging and full conversation-process recovery.
type CGAgentThought struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// Shard key (fixed to 0 in the replica environment)
	ShardingKey int32 `gorm:"column:sharding_key;not null;default:0"`
	// Agent ID
	AgentID string `gorm:"column:agent_id;size:64;not null"`
	// Agent type (e.g. manager / sub-agent)
	AgentType string `gorm:"column:agent_type;size:32;not null;default:''"`
	// User ID
	UserID int64 `gorm:"column:user_id;not null;index"`
	// Model name
	Model string `gorm:"column:model;size:64;not null;default:''"`
	// Session ID
	SessionID string `gorm:"column:session_id;index;size:64;not null"`
	// Message ID
	MsgID string `gorm:"column:msg_id;index;size:64;not null;default:''"`
	// Log ID
	LogID string `gorm:"column:log_id;size:64;not null;default:''"`
	// Product line
	Pd string `gorm:"column:pd;size:32;not null;default:''"`
	// System prompt (not included in model_input)
	SystemPrompt *string `gorm:"column:system_prompt;type:text"`
	// LLM call input (messages + tools JSON, excluding the system prompt)
	ModelInput *string `gorm:"column:model_input;type:mediumtext"`
	// LLM call output (assistant Message JSON, including tool_calls)
	ModelOutput *string `gorm:"column:model_output;type:mediumtext"`
	// Start time (millisecond timestamp)
	StartTime int64 `gorm:"column:start_time;not null;default:0"`
	// End time (millisecond timestamp)
	EndTime int64 `gorm:"column:end_time;not null;default:0"`
	// LLM call duration (milliseconds)
	ElapsedMs int64 `gorm:"column:elapsed_ms;not null;default:0"`
	// Status: 0=unknown 1=Running 2=Completed
	Status int32 `gorm:"column:status;not null;default:0"`
	// Tool-call JSON (TEXT columns cannot have a DEFAULT — MySQL strict mode raises Error 1101)
	ToolCalls string `gorm:"column:tool_calls;type:text;not null"`
	// Token statistics JSON (same as above)
	TokenCount string `gorm:"column:token_count;type:text;not null"`
	// Associated task_run ID (backfilled after dispatch; optional in the replica environment)
	TaskRunID int64 `gorm:"column:task_run_id;not null;default:0"`
	// Associated task_step ID
	TaskStepID int64 `gorm:"column:task_step_id;not null;default:0"`
	// Creation time (millisecond timestamp)
	CreateTime int64 `gorm:"column:create_time;not null"`
	// Update time (millisecond timestamp)
	UpdateTime int64 `gorm:"column:update_time;not null"`
}

// TableName returns the table name.
func (CGAgentThought) TableName() string { return "cg_agent_thought" }

// Thought status constants.
const (
	ThoughtStatusRunning   int32 = 1 // running
	ThoughtStatusCompleted int32 = 2 // completed
)
