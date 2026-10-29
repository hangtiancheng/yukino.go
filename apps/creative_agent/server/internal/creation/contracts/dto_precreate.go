package contracts

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

// PreCreateReq is the request body of /inner/creation/pre_create (API contract §3.2).
type PreCreateReq struct {
	UserID       int64  `json:"user_id"`
	SessionID    string `json:"session_id"`
	ParentStepID int64  `json:"parent_step_id"`
	TaskStepID   int64  `json:"task_step_id"`
	ProjectType  string `json:"project_type"`
	RoleType     int    `json:"role_type"`
	ProjectInput string `json:"project_input"` // JSON string, wrapped as {"input":"<concrete params JSON>"}
	RetryType    int    `json:"retry_type"`    // 0 no retry / 1 failure retry / 2 dissatisfaction retry
	CUID         string `json:"cuid"`
	IP           string `json:"ip"`
	MsgID        string `json:"msg_id"`
	IsCronTask   bool   `json:"is_cron_task"`
	LogID        string `json:"log_id"`
}

// PreCreateResp is the response body of /inner/creation/pre_create (aligned with API contract §3.2).
//
// status semantics:
//   - 0 success; project_input holds the assembled downstream operator inputs
//   - 1 failure; task_arguments.card_type indicates the failure-card type
//   - 2 input blocked by risk control; card_type = input-risk card
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
	ProjectInput  string         `json:"project_input,omitempty"` // assembled downstream operator inputs (base64)
	NewSession    bool           `json:"new_session,omitempty"`   // the upstream needs to open a new session
}

// SuccessResp builds the success response (status=0).
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

// FailCardResp builds the failure-card response (status=1).
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

// AuditFailResp builds the input-risk-block response (status=2).
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

// NewSessionResp builds the response asking the upstream to open a new session.
func NewSessionResp() *PreCreateResp {
	return &PreCreateResp{
		Errno: 0,
		Data: &PreCreateData{
			Status:   0,
			TaskType: domain.TaskTypeNewSession,
		},
	}
}

// BuildInsufficientScoreResp builds the insufficient-points response (pre_create scenario, status=1 failure card + insufficient-points card).
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
