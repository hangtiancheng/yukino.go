package contracts

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// ErrUnsupportedRole indicates pre_create received an unsupported role_type.
// Contract-layer sentinel error: returned by application; the controller maps errors based on it.
var ErrUnsupportedRole = errors.New("unsupported role_type")

// ErrInvalidInput indicates pre_create input parsing failed.
// Contract-layer sentinel error: aligned with legacy (pre_create input parse failure → 400);
// returned by application, and the controller maps errors based on it.
var ErrInvalidInput = errors.New("invalid project_input")

// CallbackResp is the unified callback response (HTTP returns 200 immediately; business logic runs asynchronously in a goroutine).
type CallbackResp struct {
	Errno  int    `json:"errno"`
	Errmsg string `json:"errmsg"`
}

// SuccessCallbackResp builds the callback success response.
func SuccessCallbackResp() *CallbackResp {
	return &CallbackResp{Errno: 0}
}

// FailCallbackResp builds the callback failure response (dispatch failure, etc.).
func FailCallbackResp(errmsg string) *CallbackResp {
	return &CallbackResp{Errno: 1, Errmsg: errmsg}
}

// CallbackReq is the request body of /inner/callback/task_detail (API contract §3.5).
type CallbackReq struct {
	TaskID             int64           `json:"task_id"`
	ProjectID          string          `json:"project_id"`
	UserID             int64           `json:"user_id"`
	FailReason         string          `json:"fail_reason"`
	TaskStatus         string          `json:"task_status"` // success / processing / failed
	ProgressPercentage int             `json:"progress_percentage"`
	SpecialInfo        json.RawMessage `json:"special_info"`
	ExtendParams       string          `json:"extend_params"` // JSON string containing task_type + callback_record_id
	EstimateTime       int             `json:"estimate_time"`
	MarioErrno         int             `json:"mario_errno"`
}

// ExtendParams are the callback pass-through fields (stuffed into extend_params with the operator inputs and echoed back verbatim on callback).
//
// Aligned with main doc §3.5: at pre_create the server stuffs it into the operator inputs — "who I am and where to notify me when done" travels with the request.
type ExtendParams struct {
	TaskType         string `json:"task_type"`          // which handler to route to (e.g. "5:mix_video")
	CallbackRecordID int64  `json:"callback_record_id"` // which callback_record to update
	CallbackStep     string `json:"callback_step"`      // only / multi single-step/multi-step callback
	TaskStepID       int64  `json:"task_step_id"`
	ParentTaskStepID int64  `json:"parent_task_step_id"`
}

// ParseExtendParams parses the callback pass-through fields.
func ParseExtendParams(raw string) (*ExtendParams, error) {
	var ep ExtendParams
	if raw == "" {
		return &ep, nil
	}
	if err := json.Unmarshal([]byte(raw), &ep); err != nil {
		return nil, fmt.Errorf("parse extend_params: %w", err)
	}
	return &ep, nil
}

// BuildExtendParamsJSON assembles the pass-through fields JSON (stuffed into the operator inputs at pre_create).
func BuildExtendParamsJSON(taskType string, recordID int64, callbackStep string, taskStepID, parentStepID int64) string {
	ep := ExtendParams{
		TaskType:         taskType,
		CallbackRecordID: recordID,
		CallbackStep:     callbackStep,
		TaskStepID:       taskStepID,
		ParentTaskStepID: parentStepID,
	}
	b, _ := json.Marshal(ep)
	return string(b)
}

// SkillTaskOutput is the raw output returned by operators (the task_output field, a JSON string).
// Aligned with §3.4: contains task_id (pipeline ID), special_info (result, base64), errno.
type SkillTaskOutput struct {
	Errno int                 `json:"errno"`
	Data  SkillTaskOutputData `json:"data"`
}

// SkillTaskOutputData is the operator's data part.
type SkillTaskOutputData struct {
	TaskID      int64  `json:"task_id"`
	TaskStatus  string `json:"task_status"`
	SpecialInfo string `json:"special_info"` // base64-encoded result
	FailReason  string `json:"fail_reason"`
}

// ParseSkillTaskOutput parses the task_output returned by operators.
func ParseSkillTaskOutput(raw string) (*SkillTaskOutput, error) {
	var out SkillTaskOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("parse task_output: %w", err)
	}
	return &out, nil
}

// --- risk callback request body (API contract §3.6) ---

// RiskCallbackReq is the request body of /inner/callback/risk (the risk-audit service callback protocol RiskAsyncSubmitRequest).
type RiskCallbackReq struct {
	BizID     string            `json:"biz_id"`   // subProduct-{bizID}
	ThirdID   string            `json:"third_id"` // the association bond = project_id
	UserID    int64             `json:"user_id"`
	Passed    bool              `json:"passed"`
	Reason    string            `json:"reason"`
	AuditType string            `json:"audit_type"`
	Extra     map[string]string `json:"extra"` // contains callback_record_id
}

// --- handle_error request body (API contract §3.7) ---

// HandleErrorReq is the request body of /inner/creation/handle_error.
type HandleErrorReq struct {
	UID       int64  `json:"uid"`
	SessionID string `json:"session_id"`
	RoleType  int    `json:"role_type"`
}

// HandleErrorResp is the response body of handle_error (aligned with §3.7).
type HandleErrorResp struct {
	Errno  int              `json:"errno"`
	Errmsg string           `json:"errmsg"`
	Data   *HandleErrorData `json:"data"`
}

// HandleErrorData is the data part of the handle_error response.
type HandleErrorData struct {
	SessionID     string         `json:"session_id"`
	UserID        int64          `json:"user_id"`
	ProjectID     string         `json:"project_id"`
	Status        string         `json:"status"`
	TaskType      string         `json:"task_type"`
	TaskArguments map[string]any `json:"task_arguments"`
}

// HandleErrorFallbackResp builds the fallback response (status=fail + fallback retry card).
func HandleErrorFallbackResp(req *HandleErrorReq) *HandleErrorResp {
	return &HandleErrorResp{
		Errno: 0,
		Data: &HandleErrorData{
			SessionID: req.SessionID,
			UserID:    req.UID,
			ProjectID: req.SessionID,
			Status:    domain.ProjectStatusFail,
			TaskType:  domain.TaskTypePushCard,
			TaskArguments: map[string]any{
				"card_type": domain.CardTypeFailedRetry,
			},
		},
	}
}
