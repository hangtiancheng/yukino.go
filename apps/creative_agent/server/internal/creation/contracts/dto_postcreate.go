package contracts

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

// --- post_create request bodies (API contract §3.4; sync/async share fields) ---

// SyncPostCreateReq is the request body of /inner/creation/sync_post_create (instant-image: get the result synchronously).
type SyncPostCreateReq struct {
	TaskStepID  int64  `json:"task_step_id"`
	SessionID   string `json:"session_id"`
	UserID      int64  `json:"user_id"`
	TaskOutput  string `json:"task_output"` // the SkillTaskOutput JSON string
	Status      string `json:"status"`      // success / fail
	CUID        string `json:"cuid"`
	IP          string `json:"ip"`
	RoleType    int    `json:"role_type"`
	ProjectType string `json:"project_type,omitempty"`
}

// AsyncPostCreateReq is the request body of /inner/creation/async_post_create (mix-cut/instant-video/image-text article: register the pipeline ID asynchronously).
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

// PostCreateResp is the unified post_create response (aligned with §3.4: CreationCallbackAgentReq).
type PostCreateResp struct {
	Errno  int             `json:"errno"`
	Errmsg string          `json:"errmsg"`
	Data   *PostCreateData `json:"data"`
}

// PostCreateData is the data part of the post_create response.
type PostCreateData struct {
	Status        string         `json:"status"`
	TaskType      string         `json:"task_type"`
	TaskArguments map[string]any `json:"task_arguments"`
}

// PostCreatePushCardResp builds the card-push response (status=success).
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

// PostCreateFailCardResp builds the failure-card response (status=fail).
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
