package model

import "encoding/json"

// CGTaskRun 是 cg_task_run 表的 GORM 模型。
//
// 状态机：Pending → Running → Finished/Failed，中间可经 Waiting（等异步产线回调）。
// 一个 task_run 对应 plan_steps 中 Agents[0]（只取第一个智能体），
// 多步串联由外部驱动（对话路径：卡片确认 + 快任务重新投递；Cron 路径：Poller 自动推进）。
type CGTaskRun struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// 任务执行业务主键（雪花 ID 整数），全局唯一
	TaskRunID int64 `gorm:"column:task_run_id;uniqueIndex;not null"`
	// 用户 ID
	UserID int64 `gorm:"column:user_id;index;not null"`
	// 智能体 ID
	AgentID string `gorm:"column:agent_id;size:64;not null"`
	// 会话 ID
	SessionID string `gorm:"column:session_id;index;size:64;not null"`
	// 关联管家思考记录 ID（复刻环境固定 0）
	ThoughtID int64 `gorm:"column:thought_id;not null;default:0"`
	// 任务类型
	Type int32 `gorm:"column:type;not null;default:0"`
	// 任务名称
	TaskName string `gorm:"column:task_name;size:256;not null;default:''"`
	// 原始用户 query
	OriginalQuery string `gorm:"column:original_query;type:text;not null"`
	// 计划步骤 JSON（PlanSteps 结构体，含完整 Agents 列表）
	PlanSteps string `gorm:"column:plan_steps;type:mediumtext;not null"`
	// 管家 message_id
	MsgID string `gorm:"column:msg_id;index;size:64;not null;default:''"`
	// 扩展字段 JSON
	Ext string `gorm:"column:ext;type:mediumtext;not null"`
	// 产品线
	Pd string `gorm:"column:pd;size:32;not null;default:''"`
	// 智能体类型
	AgentType string `gorm:"column:agent_type;size:32;not null;default:''"`
	// 状态：0=Pending, 1=Running, 2=Waiting, 3=Finished, 4=Failed
	Status int32 `gorm:"column:status;not null;default:0"`
	// 软删除：0=否 1=是
	IsDelete int8 `gorm:"column:is_delete;not null;default:0"`
	// 生产者 IP
	ProducerIP string `gorm:"column:producer_ip;size:64;not null;default:''"`
	// 消费者 IP
	ConsumerIP string `gorm:"column:consumer_ip;size:64;not null;default:''"`
	// 失败原因
	Reason string `gorm:"column:reason;type:text;not null"`
	// 开始时间（毫秒时间戳）
	StartTime int64 `gorm:"column:start_time;not null;default:0"`
	// 结束时间（毫秒时间戳）
	EndTime int64 `gorm:"column:end_time;not null;default:0"`
	// 分片键（复刻环境固定为 0）
	ShardingKey int32 `gorm:"column:sharding_key;not null;default:0"`
	// 创建时间（毫秒时间戳）
	CreateTime int64 `gorm:"column:create_time;not null"`
	// 更新时间（毫秒时间戳）
	UpdateTime int64 `gorm:"column:update_time;not null"`
}

func (CGTaskRun) TableName() string { return "cg_task_run" }

// cg_task_run 状态常量（真实系统有 6 个状态，其中 Unknown 为兜底）。
const (
	TaskRunStatusPending  int32 = 0   // 等待执行
	TaskRunStatusRunning  int32 = 1   // 执行中
	TaskRunStatusWaiting  int32 = 2   // 等待中（如等异步产线回调）
	TaskRunStatusFinished int32 = 3   // 执行成功完成
	TaskRunStatusFailed   int32 = 4   // 执行失败
	TaskRunStatusUnknown  int32 = 100 // 未知状态
)

// PlanSteps 是 cg_task_run.plan_steps 列中存储的计划 JSON 结构。
// 管家产出 new_agents_plan 时带上完整 Agents 列表，但执行器一次只消费 Agents[0]。
type PlanSteps struct {
	// AgentsPipeline 管家选的产线（对齐真实系统 PlanConfig.AgentsPipeline）。
	// 记录用 + 点卡确认路径复用；agent 链由 LLM 决定，产线限定合法 agent 集合。
	AgentsPipeline string `json:"agents_pipeline,omitempty"`
	// Agents 计划中的智能体列表（执行器只取第一个）
	Agents []PlanAgent `json:"agents"`
}

// PlanAgent 计划中的一个智能体。
type PlanAgent struct {
	// AgentID 智能体 ID（如 gold_scriptwriter / edit_master）
	AgentID string `json:"agent_id"`
	// Input 传给该智能体的输入
	Input string `json:"input"`
	// TaskType 任务类型（execute_agent 等）
	TaskType string `json:"task_type,omitempty"`
}

// ParsePlanSteps 将 plan_steps JSON 反序列化。
func (r *CGTaskRun) ParsePlanSteps() (*PlanSteps, error) {
	return ParsePlanStepsString(r.PlanSteps)
}

// SetPlanSteps 将 PlanSteps 序列化写回记录。
func (r *CGTaskRun) SetPlanSteps(p *PlanSteps) error {
	b, err := MarshalPlanSteps(p)
	if err != nil {
		return err
	}
	r.PlanSteps = string(b)
	return nil
}

// MustParsePlanSteps 解析 plan_steps，失败时返回空计划（调用方通常期望 Agents[0]）。
func MustParsePlanSteps(r *CGTaskRun) *PlanSteps {
	p, _ := r.ParsePlanSteps()
	if p == nil {
		p = &PlanSteps{}
	}
	return p
}

// MarshalPlanSteps 将 PlanSteps 序列化为 JSON。
func MarshalPlanSteps(p *PlanSteps) ([]byte, error) {
	return json.Marshal(p)
}

// ParsePlanStepsString 将 plan_steps JSON 反序列化。
func ParsePlanStepsString(s string) (*PlanSteps, error) {
	var p PlanSteps
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return nil, err
	}
	return &p, nil
}
