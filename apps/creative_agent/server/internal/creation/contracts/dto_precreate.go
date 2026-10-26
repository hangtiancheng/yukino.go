package contracts

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

// PreCreateReq 是 /inner/creation/pre_create 的请求体（接口契约 §3.2）。
type PreCreateReq struct {
	UserID       int64  `json:"user_id"`
	SessionID    string `json:"session_id"`
	ParentStepID int64  `json:"parent_step_id"`
	TaskStepID   int64  `json:"task_step_id"`
	ProjectType  string `json:"project_type"`
	RoleType     int    `json:"role_type"`
	ProjectInput string `json:"project_input"` // JSON 字符串，外层包一层 {"input":"<具体参数 JSON>"}
	RetryType    int    `json:"retry_type"`    // 0 不重试 / 1 失败重试 / 2 不满意重试
	CUID         string `json:"cuid"`
	IP           string `json:"ip"`
	MsgID        string `json:"msg_id"`
	IsCronTask   bool   `json:"is_cron_task"`
	LogID        string `json:"log_id"`
}

// PreCreateResp 是 /inner/creation/pre_create 的响应体（对齐接口契约 §3.2）。
//
// status 语义：
//   - 0 成功，project_input 里是组装好的下游算子入参
//   - 1 失败，task_arguments.card_type 指明失败卡类型
//   - 2 输入风控被拦截，card_type = 输入风控卡
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
	ProjectInput  string         `json:"project_input,omitempty"` // 组装好的下游算子入参（base64）
	NewSession    bool           `json:"new_session,omitempty"`   // 需要上游开新会话
}

// SuccessResp 构造成功响应（status=0）。
func SuccessResp(cardType string, costScore int, projectInput string) *PreCreateResp {
	return &PreCreateResp{
		Errno: 0,
		Data: &PreCreateData{
			Status:   0,
			TaskType: domain.TaskTypePushCard,
			TaskArguments: map[string]any{
				"card_type":  cardType,
				"cost_score": costScore,
			},
			ProjectInput: projectInput,
		},
	}
}

// FailCardResp 构造失败卡响应（status=1）。
func FailCardResp(cardType string, costScore int) *PreCreateResp {
	return &PreCreateResp{
		Errno: 0,
		Data: &PreCreateData{
			Status:   1,
			TaskType: domain.TaskTypePushCard,
			TaskArguments: map[string]any{
				"card_type":  cardType,
				"cost_score": costScore,
			},
		},
	}
}

// AuditFailResp 构造输入风控拦截响应（status=2）。
func AuditFailResp(cardType string) *PreCreateResp {
	return &PreCreateResp{
		Errno: 0,
		Data: &PreCreateData{
			Status:   2,
			TaskType: domain.TaskTypePushCard,
			TaskArguments: map[string]any{
				"card_type": cardType,
			},
		},
	}
}

// NewSessionResp 构造需要上游开新会话的响应。
func NewSessionResp() *PreCreateResp {
	return &PreCreateResp{
		Errno: 0,
		Data: &PreCreateData{
			Status:   0,
			TaskType: domain.TaskTypeNewSession,
		},
	}
}

// BuildInsufficientScoreResp 构造积分不足响应（pre_create 场景，status=1 失败卡 + 积分不足卡）。
func BuildInsufficientScoreResp(cost, balance int) *PreCreateResp {
	return &PreCreateResp{
		Errno: 0,
		Data: &PreCreateData{
			Status:   1,
			TaskType: domain.TaskTypePushCard,
			TaskArguments: map[string]any{
				"card_type":       domain.CardTypeInsufficientScore,
				"cost_score":      cost,
				"remaining_score": balance,
			},
		},
	}
}
