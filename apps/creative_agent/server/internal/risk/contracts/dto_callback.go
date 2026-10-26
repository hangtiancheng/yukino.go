package contracts

// RiskCallbackReq 是 /inner/callback/risk 的请求体（风险审计服务回调协议 RiskAsyncSubmitRequest）。
//
// risk 域自有的回调契约类型：controller（risk/interfaces 的 RiskHandler）与
// application（AuditOutputApp.Handle）共用本类型，与 creation 侧同名契约的
// JSON 字段逐字节一致（接口契约 §3.6）。两端各持一份同名契约是不同 bounded
// context 的自有协议；若后续希望单一来源，可考虑把回调路由处理收敛到 risk 侧。
// 两端若各自演进字段存在漂移风险，需保持 JSON 契约一致。
type RiskCallbackReq struct {
	BizID     string            `json:"biz_id"`   // subProduct-{bizID}
	ThirdID   string            `json:"third_id"` // 关联纽带 = project_id
	UserID    int64             `json:"user_id"`
	Passed    bool              `json:"passed"`
	Reason    string            `json:"reason"`
	AuditType string            `json:"audit_type"`
	Extra     map[string]string `json:"extra"` // 含 callback_record_id
}
