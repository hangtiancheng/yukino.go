package contracts

// RiskCallbackReq is the request body of /inner/callback/risk (the risk-audit service callback protocol RiskAsyncSubmitRequest).
//
// The risk domain's own callback contract type: the controller (RiskHandler in risk/interfaces) and
// the application (AuditOutputApp.Handle) share this type; its JSON fields are byte-for-byte identical to the
// same-named contract on the creation side (API contract §3.6). Each side holding its own same-named contract is the
// own protocol of different bounded contexts; if a single source is desired later, callback routing could converge to the risk side.
// If the two sides evolve fields independently there is drift risk; the JSON contract must stay consistent.
type RiskCallbackReq struct {
	BizID     string            `json:"biz_id"`   // subProduct-{bizID}
	ThirdID   string            `json:"third_id"` // the association bond = project_id
	UserID    int64             `json:"user_id"`
	Passed    bool              `json:"passed"`
	Reason    string            `json:"reason"`
	AuditType string            `json:"audit_type"`
	Extra     map[string]string `json:"extra"` // contains callback_record_id
}
