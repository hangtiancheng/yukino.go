package controller

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// RiskHandler 处理 /inner/callback/risk（视频异步风控结果回调，链路终点）。
//
// 直接持有 *AuditOutputApp，无接口跳转：HTTP 到业务一路可见。
type RiskHandler struct {
	auditOutput *service.AuditOutputApp
}

// NewRiskHandler 创建 RiskHandler。
// 具体实现由 app 装配线注入（service.AuditOutputApp）。
func NewRiskHandler(auditOutput *service.AuditOutputApp) *RiskHandler {
	return &RiskHandler{auditOutput: auditOutput}
}

// CallbackRisk 处理 /inner/callback/risk：风控回调终态落定（审计记录乐观锁 + 通知 creation）。
//
// 对齐旧 internal/handler/creation.go 的 CallbackRisk：成功时响应体为
// {"errno":0}（CallbackResp 契约 §3.6），业务成败靠 errno 区分。
func (h *RiskHandler) CallbackRisk(c *yukino.Context, _ func()) {
	var req contracts.RiskCallbackReq
	if err := c.BindJSON(&req); err != nil {
		response.Fail(c, 400, 40000, "invalid request: "+err.Error())
		return
	}
	if err := h.auditOutput.Handle(c.Request.Context(), &req); err != nil {
		response.Fail(c, 500, 50000, "risk callback failed")
		return
	}
	c.SetStatus(200)
	c.JSON(yukino.H{"errno": 0})
}
