package service

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// CompensationSink 异步风控提交失败的补偿写入（creation 域实现并注入，依赖反转）。
type CompensationSink interface {
	// InsertAuditCompensation 写一条异步风控补偿记录（biz_type=1）。幂等：同 recordID 不重复写。
	InsertAuditCompensation(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string, recordID int64) error
}

// AuditResultSink 风控审核结果落定通知（creation 域实现并注入，依赖反转）。
//
// risk 侧落定 audit_record 终态后，通过本接口通知 creation 落定 project：
//   - passed=true → project 置 done（终态短路覆盖重复通知）
//   - passed=false → 退积分 + 置 fail（points_status 幂等退，仅 used 才退）
//
// 接口定义在 risk 侧、实现由 creation.infrastructure 提供（OnAuditResultImpl），
// 保持依赖单向（creation → risk，risk 不感知 creation），组装时注入。
type AuditResultSink interface {
	Handle(ctx context.Context, projectID string, passed bool, reason string) error
}
