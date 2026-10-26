package contracts

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

// --- post_create 请求体（接口契约 §3.4，sync/async 字段一致） ---

// SyncPostCreateReq 是 /inner/creation/sync_post_create 的请求体（妙图：同步拿结果）。
type SyncPostCreateReq struct {
	TaskStepID  int64  `json:"task_step_id"`
	SessionID   string `json:"session_id"`
	UserID      int64  `json:"user_id"`
	TaskOutput  string `json:"task_output"` // SkillTaskOutput 的 JSON 字符串
	Status      string `json:"status"`      // success / fail
	CUID        string `json:"cuid"`
	IP          string `json:"ip"`
	RoleType    int    `json:"role_type"`
	ProjectType string `json:"project_type,omitempty"`
}

// AsyncPostCreateReq 是 /inner/creation/async_post_create 的请求体（混剪/妙影/图文笔记：异步登记产线 ID）。
type AsyncPostCreateReq struct {
	TaskStepID  int64  `json:"task_step_id"`
	SessionID   string `json:"session_id"`
	UserID      int64  `json:"user_id"`
	TaskOutput  string `json:"task_output"`
	Status      string `json:"status"`
	CUID        string `json:"cuid"`
	IP          string `json:"ip"`
	RoleType    int    `json:"role_type"`
	ProjectType string `json:"project_type,omitempty"`
}

// PostCreateResp 是 post_create 的统一响应（对齐 §3.4：CreationCallbackAgentReq）。
type PostCreateResp struct {
	Errno  int             `json:"errno"`
	Errmsg string          `json:"errmsg"`
	Data   *PostCreateData `json:"data"`
}

// PostCreateData 是 post_create 响应 data 部分。
type PostCreateData struct {
	Status        string         `json:"status"`
	TaskType      string         `json:"task_type"`
	TaskArguments map[string]any `json:"task_arguments"`
}

// PostCreatePushCardResp 构造推卡响应（status=success）。
func PostCreatePushCardResp(cardType string) *PostCreateResp {
	return &PostCreateResp{
		Errno: 0,
		Data: &PostCreateData{
			Status:   "success",
			TaskType: domain.TaskTypePushCard,
			TaskArguments: map[string]any{
				"card_type": cardType,
			},
		},
	}
}

// PostCreateFailCardResp 构造失败卡响应（status=fail）。
func PostCreateFailCardResp(cardType string) *PostCreateResp {
	return &PostCreateResp{
		Errno: 0,
		Data: &PostCreateData{
			Status:   "fail",
			TaskType: domain.TaskTypePushCard,
			TaskArguments: map[string]any{
				"card_type": cardType,
			},
		},
	}
}
