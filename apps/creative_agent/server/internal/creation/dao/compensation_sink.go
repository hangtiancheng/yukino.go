package dao

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	riskmodel "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
	riskapplication "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
)

// CompensationSinkImpl 实现 risk 域调用的异步风控补偿写入（CompensationSink）。
//
// 依赖反转：接口定义在 risk.application，由 creation.infrastructure 实现并注入，
// 保持依赖单向（creation → risk，risk 不感知 creation）。
//
// payload 格式与 risk.application.RiskAuditApp.RetryAsyncAudit 的解析字段严格一致：
//   - biz_id / sub_product / third_id / callback_url / user_id / record_id / content_text
//
// biz_id 同时是 cg_audit_compensation 的对账纽带（同 recordID 幂等：GetByBizID 查重）。
type CompensationSinkImpl struct {
	comp domain.CompensationRepository
}

// NewCompensationSinkImpl 创建实现。
func NewCompensationSinkImpl(comp domain.CompensationRepository) *CompensationSinkImpl {
	return &CompensationSinkImpl{comp: comp}
}

// 编译期断言：确保实现满足 risk 侧的 CompensationSink 接口。
var _ riskapplication.CompensationSink = (*CompensationSinkImpl)(nil)

// InsertAuditCompensation 异步风控提交失败时写补偿表（biz_type=1）。
// 幂等：同 recordID（biz_id）不重复写。
func (s *CompensationSinkImpl) InsertAuditCompensation(ctx context.Context, req *riskmodel.RiskSubmitRequest, subProduct string, recordID int64) error {
	// 幂等：同一 recordID 已存在补偿记录则不重复写
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
