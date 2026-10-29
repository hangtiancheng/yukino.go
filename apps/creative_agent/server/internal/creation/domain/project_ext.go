package domain

import "encoding/json"

// ProjectExt is the cross-stage data pass-through carrier, serialized into the cg_creation_project.ext field.
//
// One mix-cut is a multi-stage flow "brainstorm(2) → scriptwriter(3) → final video(5)"; stages do not pass data through memory —
// everything lives in ext, read/written by the server at each pre_create:
//   - task_step: the step ID of each (role, type) stage, key = "role_type:project_type", passed through on callback
//   - step_output: the raw product of each stage; the next stage fetches the previous stage's product from here as input
//   - order_ids: all order IDs from repeated retry deductions (for points-ledger reconciliation)
//   - msg_id: the message ID needed by result cards
type ProjectExt struct {
	TaskStep   map[string]TaskStepInfo `json:"task_step"`
	StepOutput map[string]string       `json:"step_output"`
	OrderIDs   []string                `json:"order_ids"`
	MsgID      string                  `json:"msg_id,omitempty"`
	// AsyncAuditRecordID records this project's latest video async-risk callback_record.id;
	// the risk callback /inner/callback/risk reconciles with it (written in stage 4).
	AsyncAuditRecordID int64 `json:"async_audit_record_id,omitempty"`
}

// TaskStepInfo records the step information of one (role, type) stage, passed through on callback.
type TaskStepInfo struct {
	TaskStepID       int64  `json:"task_step_id"`
	ParentTaskStepID int64  `json:"parent_task_step_id"`
	TraceParent      string `json:"trace_parent,omitempty"`
}

// --- ProjectExt serialization helpers ---

// ParseExt parses the cg_creation_project.ext JSON; returns an empty struct when empty or invalid.
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

// ToJSON serializes ProjectExt into a JSON string.
func (e *ProjectExt) ToJSON() (string, error) {
	if e == nil {
		return "", nil
	}
	b, err := json.Marshal(e)
	return string(b), err
}

// AppendOrderID appends a new order ID into order_ids (for retry-deduction reconciliation) and returns the updated JSON string.
func AppendOrderID(extJSON, orderID string) string {
	ext, err := ParseExt(extJSON)
	if err != nil {
		ext = &ProjectExt{}
	}
	for _, id := range ext.OrderIDs {
		if id == orderID {
			return extJSON // already present; idempotent
		}
	}
	ext.OrderIDs = append(ext.OrderIDs, orderID)
	out, _ := ext.ToJSON()
	return out
}

// SetStepOutput records the raw product of one (role,type) stage into step_output.
func (e *ProjectExt) SetStepOutput(roleType int, projectType, output string) {
	if e.StepOutput == nil {
		e.StepOutput = map[string]string{}
	}
	e.StepOutput[TaskTypeKey(roleType, projectType)] = output
}

// GetStepOutput fetches the raw product of the previous stage.
func (e *ProjectExt) GetStepOutput(roleType int, projectType string) string {
	if e == nil || e.StepOutput == nil {
		return ""
	}
	return e.StepOutput[TaskTypeKey(roleType, projectType)]
}
