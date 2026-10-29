package dao

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	riskmodel "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	riskapplication "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
)

// CompensationSinkImpl implements the async-risk compensation write called by the risk domain (CompensationSink).
//
// Dependency inversion: the interface is defined in risk.application, implemented and injected by creation.infrastructure,
// keeping dependencies one-way (creation → risk; risk knows nothing about creation).
//
// The payload format strictly matches the fields parsed by risk.application.RiskAuditApp.RetryAsyncAudit:
//   - biz_id / sub_product / third_id / callback_url / user_id / record_id / content_text
//
// biz_id is also the reconciliation bond of cg_audit_compensation (idempotent per recordID: GetByBizID dedup).
type CompensationSinkImpl struct {
	comp domain.CompensationRepository
}

// NewCompensationSinkImpl creates the implementation.
func NewCompensationSinkImpl(comp domain.CompensationRepository) *CompensationSinkImpl {
	return &CompensationSinkImpl{comp: comp}
}

// Compile-time assertion: the implementation satisfies the risk-side CompensationSink interface.
var _ riskapplication.CompensationSink = (*CompensationSinkImpl)(nil)

// InsertAuditCompensation writes the compensation table when async risk submission fails (biz_type=1).
// Idempotent: the same recordID (biz_id) is never written twice.
func (s *CompensationSinkImpl) InsertAuditCompensation(ctx context.Context, req *riskmodel.RiskSubmitRequest, subProduct string, recordID int64) error {
	// Idempotent: skip when a compensation record for the same recordID already exists
	if _, err := s.comp.GetByBizID(ctx, fmt.Sprintf("%d", recordID), domain.BizTypeAsyncAudit); err == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{
		"biz_id":       req.BizID,
		"sub_product":  subProduct,
		"third_id":     req.ThirdID,
		"callback_url": req.CallbackURL,
		"user_id":      req.UserID,
		"record_id":    recordID,
		"content_text": req.Content.Text,
	})
	_, err := s.comp.Insert(ctx, &domain.Compensation{
		UserID:    req.UserID,
		ProjectID: req.ThirdID,
		BizID:     fmt.Sprintf("%d", recordID),
		BizType:   domain.BizTypeAsyncAudit,
		Payload:   string(payload),
		Status:    domain.CompensationStatusPending,
	})
	return err
}
