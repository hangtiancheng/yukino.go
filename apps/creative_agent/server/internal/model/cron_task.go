package model

// CGCronTask 是 cg_cron_task 表的 GORM 模型。
//
// 定时任务会话的多 agent 自动推进索引（05-subagent.md §5.5 路径 B）。
// Poller 每 30s 扫 Running 记录，当前 task_run Finished 后 CurrentStepIndex+1 并投递下一个 agent。
type CGCronTask struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// 定时任务业务主键（雪花 ID 整数）
	CronTaskID int64 `gorm:"column:cron_task_id;uniqueIndex;not null"`
	// 会话 ID
	SessionID string `gorm:"column:session_id;index;size:64;not null"`
	// 关联 cg_task_run 中管理员的 message_id
	MsgID string `gorm:"column:msg_id;index;size:64;not null;default:''"`
	// 完整 agents 计划 JSON（与 cg_task_run.plan_steps 同构）
	PlanSteps string `gorm:"column:plan_steps;type:mediumtext;not null"`
	// 推进到第几步（0 起，指向 PlanSteps.Agents[index]）
	CurrentStepIndex int32 `gorm:"column:current_step_index;not null;default:0"`
	// 状态：0=Running 1=Done 2=Failed
	Status int32 `gorm:"column:status;not null;default:0"`
	// 失败原因
	Reason string `gorm:"column:reason;type:text;not null"`
	// 产品线
	Pd string `gorm:"column:pd;size:32;not null;default:''"`
	// 创建时间（毫秒时间戳）
	CreateTime int64 `gorm:"column:create_time;not null"`
	// 更新时间（毫秒时间戳）
	UpdateTime int64 `gorm:"column:update_time;not null"`
}

func (CGCronTask) TableName() string { return "cg_cron_task" }

// cg_cron_task 状态常量。
const (
	CronTaskStatusRunning int32 = 0 // 进行中（Poller 会推进）
	CronTaskStatusDone    int32 = 1 // 全部 agent 已执行完
	CronTaskStatusFailed  int32 = 2 // 推进失败
)
