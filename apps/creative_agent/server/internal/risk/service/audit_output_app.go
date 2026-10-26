package service

import (
	"context"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// AuditOutputApp 风控回调终态落定应用服务。
//
// 对齐 §4.5：risk 回调不再走 callback_record——阶段 3 的 AsyncAudit 插入的
// status=auditing 记录在这里做终态落定（乐观锁防并发重复回调），终态判定后
// 通过 AuditResultSink 通知 creation 落定 project（依赖反转）。
//
// 编排顺序：
//  1. 按 extra.callback_record_id 找审计记录；记录已终态 → 短路返回（幂等）
//  2. 乐观锁置终态（仅 auditing/running → done/fail，防并发重复回调）
//  3. 通知 creation 落定 project（passed → done；!passed → 退积分 + fail）
//
// 依赖全部为接口注入（domain.AuditRecordRepository / AuditResultSink），
// 不感知 creation / infrastructure，不持 gorm / redis.Client。
type AuditOutputApp struct {
	records  domain.AuditRecordRepository
	onResult AuditResultSink // 由 creation 实现并注入，依赖反转
}

// NewAuditOutputApp 创建 AuditOutputApp。
func NewAuditOutputApp(records domain.AuditRecordRepository, onResult AuditResultSink) *AuditOutputApp {
	return &AuditOutputApp{records: records, onResult: onResult}
}

// Handle 处理一次风控回调（/inner/callback/risk 入口）。
//
// 幂等语义（防 sink 失败死胡同）：record 终态短路只拦"已成功落定"的回调；终态写入
// 放在 sink 成功之后——若 sink 瞬时失败，审计记录保持 auditing，风控服务重试时会
// 重新走 sink，不会因记录已终态而短路吞掉重试。sink 侧幂等由 creation 的 project
// 终态短路 + 幂等退积分兜底（两层幂等）。
//
// 并发保护：两个并发回调都会调 sink（sink 幂等），MarkTerminal 的 CAS 保证审计记录
// 终态只写一次（后写者 RowsAffected=0，幂等忽略）。
//
// Extra 缺 callback_record_id（recordID=0）时跳过审计记录对账，仅做 sink 通知
// （旧协议兼容，最终态以 project 终态短路兜底）。
func (a *AuditOutputApp) Handle(ctx context.Context, req *contracts.RiskCallbackReq) error {
	// 1. 根据 extra.callback_record_id 找审计记录；已终态 → 幂等短路（终态意味着 sink
	//    已成功落定过，无需重做——这是与 sink 失败场景的区分点）。
	recordID := mustParseRecordID(req.Extra["callback_record_id"])
	if recordID > 0 {
		rec, err := a.records.Get(ctx, recordID)
		if err != nil {
			return err
		}
		if rec.IsTerminal() {
			return nil // 终态短路：sink 已成功落定过
		}
	}

	// 2. 先通知 creation 落定 project（sink）。sink 幂等（project 终态短路 + 幂等退积分），
	//    失败时返回错误，风控服务重试会重新走这里（审计记录尚未置终态）。
	if err := a.onResult.Handle(ctx, req.ThirdID, req.Passed, req.Reason); err != nil {
		return err
	}

	// 3. sink 成功后再乐观锁置审计记录终态（仅 auditing/running → done/fail，防并发重复）。
	//    MarkTerminal 的 bool（是否 CAS 命中）可忽略：并发重复回调时输给对方的那一方，
	//    其终态已由对方写对。
	status := domain.StatusDone
	if !req.Passed {
		status = domain.StatusFail
	}
	if recordID > 0 {
		if _, err := a.records.MarkTerminal(ctx, recordID, status, domain.StatusAuditing, domain.StatusRunning); err != nil {
			return err
		}
	}
	return nil
}
