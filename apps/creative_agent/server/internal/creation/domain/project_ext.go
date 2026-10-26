package domain

import "encoding/json"

// ProjectExt 是跨阶段数据透传载体，序列化后存于 cg_creation_project.ext 字段。
//
// 一次混剪是"脑洞(2) → 编剧(3) → 成片(5)"多阶段，阶段之间不靠内存传数据，
// 全存在 ext 里，由服务端在每次 pre_create 时读写：
//   - task_step：每个 (role, type) 阶段的步骤 ID，key = "role_type:project_type"，回调时透传
//   - step_output：每个阶段产物的原文，下一阶段从这里取上一阶段的产物做输入
//   - order_ids：重试多次扣积分的所有订单 ID（积分流水对账用）
//   - msg_id：结果卡需要的消息 ID
type ProjectExt struct {
	TaskStep   map[string]TaskStepInfo `json:"task_step"`
	StepOutput map[string]string       `json:"step_output"`
	OrderIDs   []string                `json:"order_ids"`
	MsgID      string                  `json:"msg_id,omitempty"`
	// AsyncAuditRecordID 记录本 project 最近一次视频异步风控的 callback_record.id，
	// 风控回调 /inner/callback/risk 时用它对账（阶段 4 写入）。
	AsyncAuditRecordID int64 `json:"async_audit_record_id,omitempty"`
}

// TaskStepInfo 记录某个 (role, type) 阶段的步骤信息，回调时透传。
type TaskStepInfo struct {
	TaskStepID       int64  `json:"task_step_id"`
	ParentTaskStepID int64  `json:"parent_task_step_id"`
	TraceParent      string `json:"trace_parent,omitempty"`
}

// --- ProjectExt 序列化工具 ---

// ParseExt 解析 cg_creation_project.ext 的 JSON；为空或非法时返回空结构。
func ParseExt(raw string) (*ProjectExt, error) {
	ext := &ProjectExt{}
	if raw == "" {
		return ext, nil
	}
	if err := json.Unmarshal([]byte(raw), ext); err != nil {
		return nil, err
	}
	return ext, nil
}

// ToJSON 序列化 ProjectExt 为 JSON 字符串。
func (e *ProjectExt) ToJSON() (string, error) {
	if e == nil {
		return "", nil
	}
	b, err := json.Marshal(e)
	return string(b), err
}

// AppendOrderID 把新订单号追加进 order_ids（重试扣分对账用），返回更新后的 JSON 字符串。
func AppendOrderID(extJSON, orderID string) string {
	ext, err := ParseExt(extJSON)
	if err != nil {
		ext = &ProjectExt{}
	}
	for _, id := range ext.OrderIDs {
		if id == orderID {
			return extJSON // 已存在，幂等
		}
	}
	ext.OrderIDs = append(ext.OrderIDs, orderID)
	out, _ := ext.ToJSON()
	return out
}

// SetStepOutput 记录某个 (role,type) 阶段产物的原文到 step_output。
func (e *ProjectExt) SetStepOutput(roleType int, projectType, output string) {
	if e.StepOutput == nil {
		e.StepOutput = map[string]string{}
	}
	e.StepOutput[TaskTypeKey(roleType, projectType)] = output
}

// GetStepOutput 取上一阶段产物的原文。
func (e *ProjectExt) GetStepOutput(roleType int, projectType string) string {
	if e == nil || e.StepOutput == nil {
		return ""
	}
	return e.StepOutput[TaskTypeKey(roleType, projectType)]
}
