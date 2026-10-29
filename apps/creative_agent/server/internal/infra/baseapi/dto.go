package baseapi

import "encoding/json"

// ============ DTOs replicating base-server internal/creation/contracts ============

// PreCreateReq corresponds to the base-server /inner/creation/pre_create request body.
type PreCreateReq struct {
	UserID       int64  `json:"user_id"`
	SessionID    string `json:"session_id"`
	ParentStepID int64  `json:"parent_step_id"`
	TaskStepID   int64  `json:"task_step_id"`
	ProjectType  string `json:"project_type"`
	RoleType     int    `json:"role_type"`
	ProjectInput string `json:"project_input"` // JSON string, wrapped as {"input":"<params JSON>"}
	RetryType    int    `json:"retry_type"`
	CUID         string `json:"cuid"`
	IP           string `json:"ip"`
	MsgID        string `json:"msg_id"`
	IsCronTask   bool   `json:"is_cron_task"`
	LogID        string `json:"log_id"`
}

// PreCreateResp corresponds to the base-server pre_create response body.
type PreCreateResp struct {
	Errno  int            `json:"errno"`
	Errmsg string         `json:"errmsg"`
	Data   *PreCreateData `json:"data"`
}

// PreCreateData is the data part of the pre_create response.
type PreCreateData struct {
	Status        int            `json:"status"`
	TaskType      string         `json:"task_type"`
	TaskArguments map[string]any `json:"task_arguments"`
	ProjectInput  string         `json:"project_input,omitempty"`
	NewSession    bool           `json:"new_session,omitempty"`
}

// CreateReq corresponds to the base-server /inner/creation/create request body.
type CreateReq struct {
	UserID       int64          `json:"user_id"`
	SessionID    string         `json:"session_id"`
	ProjectType  string         `json:"project_type"`
	ProjectInput map[string]any `json:"project_input"`
	Title        string         `json:"title"`
	Debug        bool           `json:"debug"`
	ProjectFrom  string         `json:"project_from"`
}

// CreateResp corresponds to the base-server create response body.
type CreateResp struct {
	Errno  int         `json:"errno"`
	Errmsg string      `json:"errmsg"`
	Data   *CreateData `json:"data"`
}

// CreateData is the data part of the create response.
type CreateData struct {
	Status        string         `json:"status"`
	TaskType      string         `json:"task_type"`
	TaskArguments map[string]any `json:"task_arguments"`
	MemberCost    int64          `json:"member_cost,omitempty"`
	RemainScore   int64          `json:"remain_score,omitempty"`
}

// AsyncPostCreateReq corresponds to the base-server /inner/creation/async_post_create request body.
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

// AsyncPostCreateResp corresponds to the base-server async_post_create response body.
type AsyncPostCreateResp struct {
	Errno  int             `json:"errno"`
	Errmsg string          `json:"errmsg"`
	Data   *PostCreateData `json:"data"`
}

// PostCreateData is the unified data part of post_create responses.
type PostCreateData struct {
	Status        string         `json:"status"`
	TaskType      string         `json:"task_type"`
	TaskArguments map[string]any `json:"task_arguments"`
}

// CallbackReq corresponds to the base-server /inner/callback/task_detail callback request body (shared by fakegen and base-server).
type CallbackReq struct {
	TaskID             int64           `json:"task_id"`
	ProjectID          string          `json:"project_id"`
	UserID             int64           `json:"user_id"`
	FailReason         string          `json:"fail_reason"`
	TaskStatus         string          `json:"task_status"` // success / processing / failed
	ProgressPercentage int             `json:"progress_percentage"`
	SpecialInfo        json.RawMessage `json:"special_info"`
	ExtendParams       string          `json:"extend_params"`
	EstimateTime       int             `json:"estimate_time"`
	MarioErrno         int             `json:"mario_errno"`
}
