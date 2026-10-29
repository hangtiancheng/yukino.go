package contracts

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

// CreateReq is the request body of /inner/creation/create (API contract §3.3).
type CreateReq struct {
	UserID       int64          `json:"user_id"`
	SessionID    string         `json:"session_id"`
	ProjectType  string         `json:"project_type"`
	ProjectInput map[string]any `json:"project_input"`
	Title        string         `json:"title"`
	Debug        bool           `json:"debug"`
	ProjectFrom  string         `json:"project_from"`
}

// CreateResp is the response body of /inner/creation/create.
//
// status semantics (aligned with §3.3):
//   - success: points deducted + work created
//   - insufficient_score: not enough points, no deduction, an insufficient-points card is produced
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

// CreateSuccessResp builds the deduction-success response.
func CreateSuccessResp(cost, remain int) *CreateResp {
	return &CreateResp{
		Errno: 0,
		Data: &CreateData{
			Status:   "success",
			TaskType: domain.TaskTypePushCard,
			TaskArguments: map[string]any{
				"card_type":  domain.CardTypeProjectProcessing,
				"cost_score": cost,
			},
			MemberCost:  int64(cost),
			RemainScore: int64(remain),
		},
	}
}

// CreateInsufficientResp builds the insufficient-points response.
func CreateInsufficientResp(cost, balance int) *CreateResp {
	return &CreateResp{
		Errno: 0,
		Data: &CreateData{
			Status:   "insufficient_score",
			TaskType: domain.TaskTypePushCard,
			TaskArguments: map[string]any{
				"card_type":       domain.CardTypeInsufficientScore,
				"cost_score":      cost,
				"remaining_score": balance,
			},
		},
	}
}
