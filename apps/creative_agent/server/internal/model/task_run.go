package model

import "encoding/json"

// CGTaskRun is the GORM model of the cg_task_run table.
//
// State machine: Pending → Running → Finished/Failed, possibly passing through Waiting (awaiting an async pipeline callback).
// One task_run corresponds to Agents[0] in plan_steps (only the first agent is taken);
// multi-step chaining is driven externally (conversation path: card confirmation + fast-task redispatch; cron path: the Poller auto-advances).
type CGTaskRun struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// Task-run business primary key (snowflake ID integer), globally unique
	TaskRunID int64 `gorm:"column:task_run_id;uniqueIndex;not null"`
	// User ID
	UserID int64 `gorm:"column:user_id;index;not null"`
	// Agent ID
	AgentID string `gorm:"column:agent_id;size:64;not null"`
	// Session ID
	SessionID string `gorm:"column:session_id;index;size:64;not null"`
	// Associated manager thought-record ID (fixed to 0 in the replica environment)
	ThoughtID int64 `gorm:"column:thought_id;not null;default:0"`
	// Task type
	Type int32 `gorm:"column:type;not null;default:0"`
	// Task name
	TaskName string `gorm:"column:task_name;size:256;not null;default:''"`
	// Original user query
	OriginalQuery string `gorm:"column:original_query;type:text;not null"`
	// Plan-steps JSON (PlanSteps struct, containing the full Agents list)
	PlanSteps string `gorm:"column:plan_steps;type:mediumtext;not null"`
	// Manager message_id
	MsgID string `gorm:"column:msg_id;index;size:64;not null;default:''"`
	// Extension fields JSON
	Ext string `gorm:"column:ext;type:mediumtext;not null"`
	// Product line
	Pd string `gorm:"column:pd;size:32;not null;default:''"`
	// Agent type
	AgentType string `gorm:"column:agent_type;size:32;not null;default:''"`
	// Status: 0=Pending, 1=Running, 2=Waiting, 3=Finished, 4=Failed
	Status int32 `gorm:"column:status;not null;default:0"`
	// Soft delete: 0=no 1=yes
	IsDelete int8 `gorm:"column:is_delete;not null;default:0"`
	// Producer IP
	ProducerIP string `gorm:"column:producer_ip;size:64;not null;default:''"`
	// Consumer IP
	ConsumerIP string `gorm:"column:consumer_ip;size:64;not null;default:''"`
	// Failure reason
	Reason string `gorm:"column:reason;type:text;not null"`
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

func (CGTaskRun) TableName() string { return "cg_task_run" }

// cg_task_run status constants (the reference system has 6 statuses; Unknown is the fallback).
const (
	TaskRunStatusPending  int32 = 0   // waiting to execute
	TaskRunStatusRunning  int32 = 1   // running
	TaskRunStatusWaiting  int32 = 2   // waiting (e.g. for an async pipeline callback)
	TaskRunStatusFinished int32 = 3   // finished successfully
	TaskRunStatusFailed   int32 = 4   // execution failed
	TaskRunStatusUnknown  int32 = 100 // unknown status
)

// PlanSteps is the plan JSON structure stored in the cg_task_run.plan_steps column.
// The manager produces new_agents_plan with the full Agents list, but the executor consumes only Agents[0] at a time.
type PlanSteps struct {
	// AgentsPipeline is the pipeline chosen by the manager (aligned with the reference system's PlanConfig.AgentsPipeline).
	// Used for recording + reused by the card-confirmation path; the agent chain is decided by the LLM, and the pipeline bounds the valid agent set.
	AgentsPipeline string `json:"agents_pipeline,omitempty"`
	// Agents is the planned agent list (the executor takes only the first)
	Agents []PlanAgent `json:"agents"`
}

// PlanAgent is one agent in the plan.
type PlanAgent struct {
	// AgentID is the agent ID (e.g. gold_scriptwriter / edit_master)
	AgentID string `json:"agent_id"`
	// Input is the input passed to the agent
	Input string `json:"input"`
	// TaskType is the task type (execute_agent, etc.)
	TaskType string `json:"task_type,omitempty"`
}

// ParsePlanSteps deserializes the plan_steps JSON.
func (r *CGTaskRun) ParsePlanSteps() (*PlanSteps, error) {
	return ParsePlanStepsString(r.PlanSteps)
}

// SetPlanSteps serializes PlanSteps back into the record.
func (r *CGTaskRun) SetPlanSteps(p *PlanSteps) error {
	b, err := MarshalPlanSteps(p)
	if err != nil {
		return err
	}
	r.PlanSteps = string(b)
	return nil
}

// MustParsePlanSteps parses plan_steps, returning an empty plan on failure (callers usually expect Agents[0]).
func MustParsePlanSteps(r *CGTaskRun) *PlanSteps {
	p, _ := r.ParsePlanSteps()
	if p == nil {
		p = &PlanSteps{}
	}
	return p
}

// MarshalPlanSteps serializes PlanSteps to JSON.
func MarshalPlanSteps(p *PlanSteps) ([]byte, error) {
	return json.Marshal(p)
}

// ParsePlanStepsString deserializes the plan_steps JSON.
func ParsePlanStepsString(s string) (*PlanSteps, error) {
	var p PlanSteps
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return nil, err
	}
	return &p, nil
}
