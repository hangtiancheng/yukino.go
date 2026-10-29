package domain

import "context"

// AuditImage is a single image.
type AuditImage struct {
	URL string `json:"url"`
}

// AuditContent is the content to audit: text or image, one of the two (video goes through Async).
type AuditContent struct {
	Text  string       `json:"text,omitempty"`
	Image []AuditImage `json:"image,omitempty"`
}

// RiskSubmitRequest is the audit-submission request (shared by sync/async).
type RiskSubmitRequest struct {
	UserID      int64             `json:"user_id"`
	ThirdID     string            `json:"third_id"`
	BizID       string            `json:"biz_id"`
	Content     AuditContent      `json:"content"`
	CallbackURL string            `json:"callback_url,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
}

// RiskExtra holds the common source fields of audit requests (user/IP/step, etc., used for risk-service profiling).
type RiskExtra struct {
	UserID     int64
	CuID       string
	IP         string
	TaskStepID int64
}

// NewRiskSubmitRequest builds an audit-submission request.
func NewRiskSubmitRequest(userID int64, thirdID, bizID, text string) *RiskSubmitRequest {
	return &RiskSubmitRequest{
		UserID:  userID,
		ThirdID: thirdID,
		BizID:   bizID,
		Content: AuditContent{Text: text},
	}
}

// AuditBackend is the injectable backend for risk decisions.
// Return semantics:
//   - err != nil → the risk request failed (system error)
//   - passed=false + reason → content rejected (risk hit)
//   - passed=true → passed
type AuditBackend interface {
	Submit(ctx context.Context, req *RiskSubmitRequest, subProduct string) (passed bool, reason string, err error)
}
