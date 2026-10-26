package contracts

import "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

// CreateReq 是 /inner/creation/create 的请求体（接口契约 §3.3）。
type CreateReq struct {
	UserID       int64          `json:"user_id"`
	SessionID    string         `json:"session_id"`
	ProjectType  string         `json:"project_type"`
	ProjectInput map[string]any `json:"project_input"`
	Title        string         `json:"title"`
	Debug        bool           `json:"debug"`
	ProjectFrom  string         `json:"project_from"`
}

// CreateResp 是 /inner/creation/create 的响应体。
//
// status 语义（对齐 §3.3）：
//   - success 扣分成功 + 作品已建
//   - insufficient_score 积分不足，不扣分，出积分不足卡
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

// CreateSuccessResp 构造扣分成功响应。
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

// CreateInsufficientResp 构造积分不足响应。
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
