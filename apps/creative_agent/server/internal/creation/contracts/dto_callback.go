package contracts

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// ErrUnsupportedRole 表示 pre_create 收到不支持的 role_type。
// 契约层哨兵错误：application 返回、controller 据此做错误映射。
var ErrUnsupportedRole = errors.New("unsupported role_type")

// ErrInvalidInput 表示 pre_create 输入解析失败。
// 契约层哨兵错误：对齐 legacy（pre_create 输入解析失败 → 400），
// application 返回、controller 据此做错误映射。
var ErrInvalidInput = errors.New("invalid project_input")

// CallbackResp 是回调接口的统一响应（HTTP 立即返回 200，业务逻辑在 goroutine 异步执行）。
type CallbackResp struct {
	Errno  int    `json:"errno"`
	Errmsg string `json:"errmsg"`
}

// SuccessCallbackResp 构造回调成功响应。
func SuccessCallbackResp() *CallbackResp {
	return &CallbackResp{Errno: 0}
}

// FailCallbackResp 构造回调失败响应（分发失败等）。
func FailCallbackResp(errmsg string) *CallbackResp {
	return &CallbackResp{Errno: 1, Errmsg: errmsg}
}

// CallbackReq 是 /inner/callback/task_detail 的请求体（接口契约 §3.5）。
type CallbackReq struct {
	TaskID             int64           `json:"task_id"`
	ProjectID          string          `json:"project_id"`
	UserID             int64           `json:"user_id"`
	FailReason         string          `json:"fail_reason"`
	TaskStatus         string          `json:"task_status"` // success / processing / failed
	ProgressPercentage int             `json:"progress_percentage"`
	SpecialInfo        json.RawMessage `json:"special_info"`
	ExtendParams       string          `json:"extend_params"` // JSON 字符串，含 task_type + callback_record_id
	EstimateTime       int             `json:"estimate_time"`
	MarioErrno         int             `json:"mario_errno"`
}

// ExtendParams 是回调透传字段（随算子入参塞进 extend_params，回调时原样带回）。
//
// 对齐主文档 §3.5：pre_create 时服务端把它塞进算子入参，"我是谁、处理完通知我哪里"随请求带过去。
type ExtendParams struct {
	TaskType         string `json:"task_type"`          // 路由到哪个 handler（如 "5:mix_video"）
	CallbackRecordID int64  `json:"callback_record_id"` // 更新哪条 callback_record
	CallbackStep     string `json:"callback_step"`      // only / multi 单步/多步回调
	TaskStepID       int64  `json:"task_step_id"`
	ParentTaskStepID int64  `json:"parent_task_step_id"`
}

// ParseExtendParams 解析回调透传字段。
func ParseExtendParams(raw string) (*ExtendParams, error) {
	var ep ExtendParams
	if raw == "" {
		return &ep, nil
	}
	if err := json.Unmarshal([]byte(raw), &ep); err != nil {
		return nil, fmt.Errorf("解析 extend_params 失败: %w", err)
	}
	return &ep, nil
}

// BuildExtendParamsJSON 组装透传字段 JSON（pre_create 时塞进算子入参）。
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

// SkillTaskOutput 是算子返回的原始输出（task_output 字段，JSON 字符串）。
// 对齐 §3.4：含 task_id（产线 ID）、special_info（结果，base64）、errno。
type SkillTaskOutput struct {
	Errno int                 `json:"errno"`
	Data  SkillTaskOutputData `json:"data"`
}

// SkillTaskOutputData 是算子的 data 部分。
type SkillTaskOutputData struct {
	TaskID      int64  `json:"task_id"`
	TaskStatus  string `json:"task_status"`
	SpecialInfo string `json:"special_info"` // base64 编码的结果
	FailReason  string `json:"fail_reason"`
}

// ParseSkillTaskOutput 解析算子返回的 task_output。
func ParseSkillTaskOutput(raw string) (*SkillTaskOutput, error) {
	var out SkillTaskOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("解析 task_output 失败: %w", err)
	}
	return &out, nil
}

// --- risk 回调请求体（接口契约 §3.6） ---

// RiskCallbackReq 是 /inner/callback/risk 的请求体（风险审计服务回调协议 RiskAsyncSubmitRequest）。
type RiskCallbackReq struct {
	BizID     string            `json:"biz_id"`   // subProduct-{bizID}
	ThirdID   string            `json:"third_id"` // 关联纽带 = project_id
	UserID    int64             `json:"user_id"`
	Passed    bool              `json:"passed"`
	Reason    string            `json:"reason"`
	AuditType string            `json:"audit_type"`
	Extra     map[string]string `json:"extra"` // 含 callback_record_id
}

// --- handle_error 请求体（接口契约 §3.7） ---

// HandleErrorReq 是 /inner/creation/handle_error 的请求体。
type HandleErrorReq struct {
	UID       int64  `json:"uid"`
	SessionID string `json:"session_id"`
	RoleType  int    `json:"role_type"`
}

// HandleErrorResp 是 handle_error 的响应体（对齐 §3.7）。
type HandleErrorResp struct {
	Errno  int              `json:"errno"`
	Errmsg string           `json:"errmsg"`
	Data   *HandleErrorData `json:"data"`
}

// HandleErrorData 是 handle_error 响应 data 部分。
type HandleErrorData struct {
	SessionID     string         `json:"session_id"`
	UserID        int64          `json:"user_id"`
	ProjectID     string         `json:"project_id"`
	Status        string         `json:"status"`
	TaskType      string         `json:"task_type"`
	TaskArguments map[string]any `json:"task_arguments"`
}

// HandleErrorFallbackResp 构造兜底响应（status=fail + 兜底重试卡）。
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
