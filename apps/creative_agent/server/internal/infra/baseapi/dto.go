package baseapi

import "encoding/json"

// ============ 复刻 base-server internal/creation/contracts 的 DTO ============

// PreCreateReq 对应 base-server /inner/creation/pre_create 请求体。
type PreCreateReq struct {
	UserID       int64  `json:"user_id"`
	SessionID    string `json:"session_id"`
	ParentStepID int64  `json:"parent_step_id"`
	TaskStepID   int64  `json:"task_step_id"`
	ProjectType  string `json:"project_type"`
	RoleType     int    `json:"role_type"`
	ProjectInput string `json:"project_input"` // JSON 字符串，外层 {"input":"<参数 JSON>"}
	RetryType    int    `json:"retry_type"`
	CUID         string `json:"cuid"`
	IP           string `json:"ip"`
	MsgID        string `json:"msg_id"`
	IsCronTask   bool   `json:"is_cron_task"`
	LogID        string `json:"log_id"`
}

// PreCreateResp 对应 base-server pre_create 响应体。
type PreCreateResp struct {
	Errno  int            `json:"errno"`
	Errmsg string         `json:"errmsg"`
	Data   *PreCreateData `json:"data"`
}

// PreCreateData 是 pre_create 响应 data 部分。
type PreCreateData struct {
	Status        int            `json:"status"`
	TaskType      string         `json:"task_type"`
	TaskArguments map[string]any `json:"task_arguments"`
	ProjectInput  string         `json:"project_input,omitempty"`
	NewSession    bool           `json:"new_session,omitempty"`
}

// CreateReq 对应 base-server /inner/creation/create 请求体。
type CreateReq struct {
	UserID       int64          `json:"user_id"`
	SessionID    string         `json:"session_id"`
	ProjectType  string         `json:"project_type"`
	ProjectInput map[string]any `json:"project_input"`
	Title        string         `json:"title"`
	Debug        bool           `json:"debug"`
	ProjectFrom  string         `json:"project_from"`
}

// CreateResp 对应 base-server create 响应体。
type CreateResp struct {
	Errno  int         `json:"errno"`
	Errmsg string      `json:"errmsg"`
	Data   *CreateData `json:"data"`
}

// CreateData 是 create 响应 data 部分。
type CreateData struct {
	Status        string         `json:"status"`
	TaskType      string         `json:"task_type"`
	TaskArguments map[string]any `json:"task_arguments"`
	MemberCost    int64          `json:"member_cost,omitempty"`
	RemainScore   int64          `json:"remain_score,omitempty"`
}

// AsyncPostCreateReq 对应 base-server /inner/creation/async_post_create 请求体。
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

// AsyncPostCreateResp 对应 base-server async_post_create 响应体。
type AsyncPostCreateResp struct {
	Errno  int             `json:"errno"`
	Errmsg string          `json:"errmsg"`
	Data   *PostCreateData `json:"data"`
}

// PostCreateData 是 post_create 统一响应 data 部分。
type PostCreateData struct {
	Status        string         `json:"status"`
	TaskType      string         `json:"task_type"`
	TaskArguments map[string]any `json:"task_arguments"`
}

// CallbackReq 对应 base-server /inner/callback/task_detail 回调请求体（fakegen 与 base-server 通用）。
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
