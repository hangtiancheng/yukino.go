package domain

// CallbackRecord 任务回调记录（cg_task_callback_record）。
// task_type 形如 "3:mix_video"、"5:mix_video"（非 risk: 前缀）。
type CallbackRecord struct {
	ID               uint64
	UserID           int64
	ProjectID        string
	TaskStepID       int64
	TaskType         string
	TaskInput        string // JSON
	TaskOutput       string // JSON
	Status           string
	ProductionLineID string
	CallbackTime     int64
	CreateTime       int64
	// UpdateTime 最近一次变更时间（秒级）。风控治理轮询器按它做"更新游标"（游标 B），
	// 捕异步风控命中：auditing/running → fail 的 UPDATE 会刷新该时间戳。
	UpdateTime int64
}

// IsTerminal 是否终态（done/fail）。
func (r *CallbackRecord) IsTerminal() bool {
	return r.Status == ProjectStatusDone || r.Status == ProjectStatusFail
}
