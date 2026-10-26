package model

// CGTaskStep 是 cg_task_step 表的 GORM 模型。
//
// 职责：子智能体执行的每个步骤记录。支持树形结构（parent_step_id 自引用），
// 一道子智能体调用可以有多个子步骤（工具调用）。
// step_output 是 ③ 对话 Worker 回流前端的唯一数据源——poll 轮询此列推 SSE。
type CGTaskStep struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// 步骤业务主键（雪花 ID 整数），全局唯一
	TaskStepID int64 `gorm:"column:task_step_id;uniqueIndex;not null"`
	// 关联 cg_task_run.task_run_id
	TaskRunID int64 `gorm:"column:task_run_id;index;not null"`
	// 父步骤 ID（0=顶层步骤，即子智能体级）
	ParentStepID int64 `gorm:"column:parent_step_id;index;not null;default:0"`
	// 用户 ID
	UserID int64 `gorm:"column:user_id;not null"`
	// 智能体 ID
	AgentID string `gorm:"column:agent_id;size:64;not null"`
	// 会话 ID
	SessionID string `gorm:"column:session_id;index;size:64;not null"`
	// 定时任务 ID（非定时为 0）
	CronTaskID int64 `gorm:"column:cron_task_id;not null;default:0"`
	// 步骤名称（子智能体级=agent 名，工具调用级=tool 名）
	StepName string `gorm:"column:step_name;size:256;not null;default:''"`
	// 步骤输入 JSON
	StepInput string `gorm:"column:step_input;type:mediumtext;not null"`
	// 步骤输出 JSON（供 poll 回流读取）
	StepOutput string `gorm:"column:step_output;type:mediumtext;not null"`
	// 中间结果 JSON（need_wait=Yes 时的中间产物）
	IntermediateResult string `gorm:"column:intermediate_result;type:mediumtext;not null"`
	// 状态：0=Pending, 1=Running, 2=Interrupted, 3=Finished, 4=Failed, 5=Skipped
	Status int32 `gorm:"column:status;not null;default:0"`
	// 是否需要等待：0=No 1=Yes（触发了异步产线，等产线回调）
	NeedWait int8 `gorm:"column:need_wait;not null;default:0"`
	// 产品线
	Pd string `gorm:"column:pd;size:32;not null;default:''"`
	// 智能体类型
	AgentType string `gorm:"column:agent_type;size:32;not null;default:''"`
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

func (CGTaskStep) TableName() string { return "cg_task_step" }

// cg_task_step 状态常量（真实系统有 6 个状态）。
const (
	TaskStepStatusPending     int32 = 0 // 等待执行
	TaskStepStatusRunning     int32 = 1 // 执行中
	TaskStepStatusInterrupted int32 = 2 // 中断（HIL 断点等待用户操作）
	TaskStepStatusFinished    int32 = 3 // 已完成
	TaskStepStatusFailed      int32 = 4 // 执行失败
	TaskStepStatusSkipped     int32 = 5 // 已跳过
)

// NeedWait 常量。
const (
	NeedWaitNo  int8 = 0 // 子智能体自己产出了终态结果
	NeedWaitYes int8 = 1 // 触发了异步产线，等产线回调
)
