package model

// CGAgentThought 是 cg_agent_thought 表的 GORM 模型。
// 记录管家/子智能体每一轮 LLM 调用的输入输出轨迹（对齐真实系统 cg_agent_thought），
// 供调试与完整对话过程恢复。
type CGAgentThought struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// 分片键（复刻环境固定为 0）
	ShardingKey int32 `gorm:"column:sharding_key;not null;default:0"`
	// 智能体 ID
	AgentID string `gorm:"column:agent_id;size:64;not null"`
	// 智能体类型（如管家 / 子智能体）
	AgentType string `gorm:"column:agent_type;size:32;not null;default:''"`
	// 用户 ID
	UserID int64 `gorm:"column:user_id;not null;index"`
	// 模型名
	Model string `gorm:"column:model;size:64;not null;default:''"`
	// 会话 ID
	SessionID string `gorm:"column:session_id;index;size:64;not null"`
	// 消息 ID
	MsgID string `gorm:"column:msg_id;index;size:64;not null;default:''"`
	// 日志 ID
	LogID string `gorm:"column:log_id;size:64;not null;default:''"`
	// 产品线
	Pd string `gorm:"column:pd;size:32;not null;default:''"`
	// System prompt（不含于 model_input）
	SystemPrompt *string `gorm:"column:system_prompt;type:text"`
	// LLM 调用输入（messages + tools JSON，不含 system prompt）
	ModelInput *string `gorm:"column:model_input;type:mediumtext"`
	// LLM 调用输出（assistant Message JSON，含 tool_calls）
	ModelOutput *string `gorm:"column:model_output;type:mediumtext"`
	// 开始时间（毫秒时间戳）
	StartTime int64 `gorm:"column:start_time;not null;default:0"`
	// 结束时间（毫秒时间戳）
	EndTime int64 `gorm:"column:end_time;not null;default:0"`
	// LLM 调用耗时（毫秒）
	ElapsedMs int64 `gorm:"column:elapsed_ms;not null;default:0"`
	// 状态：0=未知 1=Running 2=Completed
	Status int32 `gorm:"column:status;not null;default:0"`
	// 工具调用 JSON（TEXT 列不能带 DEFAULT——MySQL 严格模式报 Error 1101）
	ToolCalls string `gorm:"column:tool_calls;type:text;not null"`
	// token 统计 JSON（同上）
	TokenCount string `gorm:"column:token_count;type:text;not null"`
	// 关联 task_run ID（投递后回填，复刻环境可选）
	TaskRunID int64 `gorm:"column:task_run_id;not null;default:0"`
	// 关联 task_step ID
	TaskStepID int64 `gorm:"column:task_step_id;not null;default:0"`
	// 创建时间（毫秒时间戳）
	CreateTime int64 `gorm:"column:create_time;not null"`
	// 更新时间（毫秒时间戳）
	UpdateTime int64 `gorm:"column:update_time;not null"`
}

// TableName 返回表名。
func (CGAgentThought) TableName() string { return "cg_agent_thought" }

// Thought 状态常量。
const (
	ThoughtStatusRunning   int32 = 1 // 执行中
	ThoughtStatusCompleted int32 = 2 // 完成
)
