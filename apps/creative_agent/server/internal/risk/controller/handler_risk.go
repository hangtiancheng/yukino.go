package controller

import (
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/response"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// RiskHandler handles /inner/callback/risk (the async video risk-control result callback; the chain's endpoint).
//
// It holds *AuditOutputApp directly, with no interface hops: HTTP to business is visible all the way.
type RiskHandler struct {
	auditOutput *service.AuditOutputApp
}

// NewRiskHandler creates a RiskHandler.
// The concrete implementation is injected by the app assembly line (service.AuditOutputApp).
func NewRiskHandler(auditOutput *service.AuditOutputApp) *RiskHandler {
	return &RiskHandler{auditOutput: auditOutput}
}

// CallbackRisk handles /inner/callback/risk: settle the risk-callback terminal state (audit-record optimistic lock + notify creation).
//
// Aligned with the old internal/handler/creation.go CallbackRisk: on success the response body is
// {"errno":0} (CallbackResp contract §3.6); business success/failure is distinguished by errno.
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
