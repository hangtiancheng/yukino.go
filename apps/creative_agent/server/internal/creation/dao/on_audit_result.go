package dao

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/service"
	riskservice "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/service"
)

// OnAuditResultImpl 实现 risk 域调用的审核结果通知（AuditResultSink）。
//
// 依赖反转：接口定义在 risk.application，由 creation.infrastructure 实现并注入，
// 保持依赖单向（creation → risk，risk 不感知 creation）。
//
// 对齐 spec §5.2：passed → project 置 done；!passed → 退积分 + 置 fail。
// 幂等：project 终态短路（MarkDone/MarkFail 终态不变）+ points 幂等退（仅 used 才退）。
type OnAuditResultImpl struct {
	projects domain.ProjectRepository
	points   service.PointsPort
}

// NewOnAuditResultImpl 创建实现。
func NewOnAuditResultImpl(projects domain.ProjectRepository, points service.PointsPort) *OnAuditResultImpl {
	return &OnAuditResultImpl{projects: projects, points: points}
}

// 编译期断言：确保实现满足 risk 侧的 AuditResultSink 接口。
var _ riskservice.AuditResultSink = (*OnAuditResultImpl)(nil)

// Handle 处理一次风控审核结果：passed → project done；!passed → 退积分 + fail。
//
// 幂等：project 终态短路 + points 幂等退（仅 used 才退）。
// ⚠️ CAS 落定（防 audit→done 丢失更新竞态）：成片回调 handler 也在 goroutine 里写 project
// （置 audit + ext.async_audit_record_id）。若这里用无条件全字段 Update，handler 陈旧快照
// 可能把已落定的 done 覆盖回 audit。因此用 UpdateIfStatus(project, audit) 做 DB 级 CAS——
// 仅当 DB 状态仍是 audit 才写入终态；CAS 失败表示状态已被推进（如重复回调已落定），幂等短路。
func (h *OnAuditResultImpl) Handle(ctx context.Context, projectID string, passed bool, reason string) error {
	project, err := h.projects.GetByProjectID(ctx, projectID)
	if err != nil {
		return err
	}
	if project.IsTerminal() {
		return nil // 终态短路
	}
	var ok bool
	if passed {
		if !project.MarkDone() {
			return nil
		}
		// CAS 期望 audit 或 running：正常链路 handler 已置 audit；旧协议（无 recordID
		// 对账）project 可能仍是 running。两者都允许落定 done，杜绝覆盖竞态。
		ok, err = h.projects.UpdateIfStatus(ctx, project, domain.ProjectStatusAudit, domain.ProjectStatusRunning)
	} else {
		var changed bool
		changed, err = project.MarkFail(contracts.BuildFailReason("输出风控拦截", reason), func() error {
			return h.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
		})
		if err != nil || !changed {
			return err
		}
		ok, err = h.projects.UpdateIfStatus(ctx, project, domain.ProjectStatusAudit, domain.ProjectStatusRunning)
	}
	if err != nil {
		return err
	}
	if !ok {
		// CAS 失败：状态已被并发推进。若本次落定 done/fail 被 handler 抢占，放弃即可；
		// 若 handler 的 running→audit 还没落库（handler 与 risk 回调并发），audit 已写入
		// 的快照不覆盖，交由下一条回调或超时兜底。返回 nil 幂等。
		return nil
	}
	return nil
}
