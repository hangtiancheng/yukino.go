package domain

import "context"

// AuditImage 单张图片。
type AuditImage struct {
	URL string `json:"url"`
}

// AuditContent 待审内容：文本与图片二选一（视频走 Async）。
type AuditContent struct {
	Text  string       `json:"text,omitempty"`
	Image []AuditImage `json:"image,omitempty"`
}

// RiskSubmitRequest 送审请求（同步/异步共用）。
type RiskSubmitRequest struct {
	UserID      int64             `json:"user_id"`
	ThirdID     string            `json:"third_id"`
	BizID       string            `json:"biz_id"`
	Content     AuditContent      `json:"content"`
	CallbackURL string            `json:"callback_url,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
}

// RiskExtra 送审请求的公共来源字段（用户/IP/步骤等，用于风控服务画像）。
type RiskExtra struct {
	UserID     int64
	CuID       string
	IP         string
	TaskStepID int64
}

// NewRiskSubmitRequest 构造送审请求。
func NewRiskSubmitRequest(userID int64, thirdID, bizID, text string) *RiskSubmitRequest {
	return &RiskSubmitRequest{
		UserID:  userID,
		ThirdID: thirdID,
		BizID:   bizID,
		Content: AuditContent{Text: text},
	}
}

// AuditBackend 风控判定可注入后端。
// 返回语义：
//   - err != nil → 风控请求失败（系统错误）
//   - passed=false + reason → 内容不通过（命中风控）
//   - passed=true → 通过
type AuditBackend interface {
	Submit(ctx context.Context, req *RiskSubmitRequest, subProduct string) (passed bool, reason string, err error)
}
