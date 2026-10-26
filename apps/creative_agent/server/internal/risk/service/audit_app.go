package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/dao"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// RiskAuditApp 风控送审应用服务。
//
// 编排送审三态：
//   - SyncAudit 同步送审（文本/图片），阻塞等待结果，立即返回
//   - AsyncAudit 异步送审（视频），提交即返回，结果由回调闭环
//   - RetryAsyncAudit 异步补偿重试入口（补偿 job 调用）
//
// 依赖全部为接口注入（domain.AuditBackend / domain.AuditRecordRepository /
// CompensationSink / domain.BlacklistChecker），不持有 gorm / redis.Client。
type RiskAuditApp struct {
	backend   domain.AuditBackend
	records   domain.AuditRecordRepository
	sink      CompensationSink
	blacklist domain.BlacklistChecker // nil 时跳过黑名单检查、风控照常
}

// NewRiskAuditApp 创建送审应用服务。
func NewRiskAuditApp(backend domain.AuditBackend, records domain.AuditRecordRepository, sink CompensationSink, blacklist domain.BlacklistChecker) *RiskAuditApp {
	return &RiskAuditApp{
		backend:   backend,
		records:   records,
		sink:      sink,
		blacklist: blacklist,
	}
}

// SyncAudit 文本/图片同步风控：阻塞等待结果，立即返回。
//
// 返回：
//   - passed=false + err=non-nil → 风控请求失败（系统错误）
//   - passed=false + reason=non-empty → 内容不通过（命中风控）
//   - passed=true → 通过
func (a *RiskAuditApp) SyncAudit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	// 图片风控：拆分为每次 1 张，限速派发 + 一票否决（派发/聚合策略收敛在 domain.SyncAuditImages）
	if len(req.Content.Image) > 0 {
		return domain.SyncAuditImages(ctx, a.backend, req, subProduct)
	}
	// 文本风控：单次请求
	return a.doSyncRequest(ctx, req, subProduct)
}

// doSyncRequest 实际调用风控后端，返回 (passed, reason, err)。
func (a *RiskAuditApp) doSyncRequest(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	return a.backend.Submit(ctx, req, subProduct)
}

// AsyncAudit 视频异步风控：提交后立即返回，结果由 /inner/callback/risk 闭环。
//
// 两个闭环纽带：
//  1. CallbackURL：把"审完通知我哪里"随请求带过去
//  2. extra.callback_record_id：回调时透传回来，服务端用它找到是哪条记录对账
//
// 流程：先插一条 status=auditing 的审计记录 → 提交风控请求（带回调参数）→
// 提交失败写补偿表（sink，Phase 3 的补偿 job 处理）→ 返回 recordID。
func (a *RiskAuditApp) AsyncAudit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string, callbackURL string) (int64, error) {
	// 1. 先插一条 status=auditing 的审计记录（task_type 形如 "risk:async:{subProduct}"）
	insertedID, err := a.records.Insert(ctx, &domain.AuditRecord{
		UserID:    req.UserID,
		ProjectID: req.ThirdID,
		TaskType:  "risk:async:" + subProduct,
		Status:    domain.StatusAuditing,
	})
	if err != nil {
		return 0, err
	}
	recordID := int64(insertedID)

	// 2. 提交风控请求，带上回调闭环参数
	req.CallbackURL = callbackURL
	if req.Extra == nil {
		req.Extra = map[string]string{}
	}
	req.Extra["callback_record_id"] = strconv.FormatInt(recordID, 10)

	if err := a.submitAuditRequest(ctx, req, subProduct); err != nil {
		// 提交失败 → 写补偿表，等重试 job 处理（sink 未注入时静默跳过）
		if a.sink != nil {
			_ = a.sink.InsertAuditCompensation(ctx, req, subProduct, recordID)
		}
		return 0, err
	}

	return recordID, nil
}

// submitAuditRequest 提交异步风控请求到后端（返回 err 表示提交失败）。
//
// 本地复现的 backend 判定是同步的，异步提交语义用「在审计记录上标记已提交」模拟：
// 把提交状态写回 auditing 记录的 task_input（审计对账可见），而非真的阻塞等待。
// 真实接入时替换为 POST /async/audit（带 CallbackURL）。
func (a *RiskAuditApp) submitAuditRequest(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// 校验：异步必须有内容（视频/文本），空请求视为提交失败
	if len(req.Content.Text) == 0 && len(req.Content.Image) == 0 {
		return fmt.Errorf("async audit content is empty")
	}

	// 把本次提交的入参（subProduct + callback 纽带）记进记录，供对账
	submitted, _ := json.Marshal(map[string]any{
		"sub_product":        subProduct,
		"callback_url":       req.CallbackURL,
		"callback_record_id": req.Extra["callback_record_id"],
	})
	return a.records.Update(ctx, mustParseRecordID(req.Extra["callback_record_id"]), map[string]any{
		"task_input": string(submitted),
	})
}

// RetryAsyncAudit 供补偿 job 调用的异步风控重试入口。
//
// payload 是补偿写入的 JSON，包含：
//   - biz_id / sub_product / third_id / callback_url / user_id / record_id / content_text
//
// 重试语义：**复用原始 auditing 记录**走 submitAuditRequest（而非重新走完整 AsyncAudit——
// 否则内容仍为空会再次写补偿记录造成死循环）。content_text 为空视为重试失败，
// 由补偿 job 累加 retry_count，超上限放弃时走超时兜底退积分。
func (a *RiskAuditApp) RetryAsyncAudit(ctx context.Context, payload map[string]any) error {
	subProduct := asString(payload["sub_product"])
	if subProduct == "" {
		return fmt.Errorf("retry async audit: missing sub_product")
	}
	recordID := asInt64(payload["record_id"])
	if recordID <= 0 {
		return fmt.Errorf("retry async audit: missing record_id")
	}
	req := &domain.RiskSubmitRequest{
		UserID:      asInt64(payload["user_id"]),
		ThirdID:     asString(payload["third_id"]),
		BizID:       asString(payload["biz_id"]),
		CallbackURL: asString(payload["callback_url"]),
		Content:     domain.AuditContent{Text: asString(payload["content_text"])},
	}
	if req.Content.Text == "" {
		return fmt.Errorf("retry async audit: missing content")
	}
	req.Extra = map[string]string{"callback_record_id": strconv.FormatInt(recordID, 10)}
	return a.submitAuditRequest(ctx, req, subProduct)
}

// IsBlacklisted 送审前黑名单检查（blacklist 未注入或 subProduct 为空时放行）。
func (a *RiskAuditApp) IsBlacklisted(ctx context.Context, userID int64, subProduct string) bool {
	return a.checkRiskBlacklist(ctx, userID, subProduct)
}

// NewSyncAuditRecorder 创建一次同步风控的合并记录器（risk 侧实现）。
//
// creation.application 通过 RiskPort 调用本方法拿到 recorder 实现（跨 context 依赖反转：
// 接口定义在 risk.domain，实现在 risk.infrastructure，creation 只依赖接口不依赖实现）。
// taskType 形如 "risk:{role}:{type}:{stage}"（如 "risk:5:mix_video:precreate"），
// 供审计对账按 (role,type) 检索。
func (a *RiskAuditApp) NewSyncAuditRecorder(ctx context.Context, userID int64, projectID string, taskStepID int64, taskType string) domain.SyncAuditRecorder {
	return dao.NewSyncAuditRecorder(ctx, userID, projectID, taskStepID, taskType, a.backend, a.records)
}

// checkRiskBlacklist 送审前黑名单检查：命中返回 true（拦截）；
// blacklist 未注入（nil）或 subProduct 为空时放行。
func (a *RiskAuditApp) checkRiskBlacklist(ctx context.Context, userID int64, subProduct string) bool {
	if a.blacklist == nil || subProduct == "" {
		return false
	}
	return a.blacklist.IsBlacklisted(ctx, userID, subProduct)
}

// --- 工具函数 ---

// mustParseRecordID 解析 extra 里的 callback_record_id；异常时返回 0（占位，正常流程不会走到）。
func mustParseRecordID(s string) uint64 {
	id, _ := strconv.ParseUint(s, 10, 64)
	return id
}

// asString 从 map 里安全取 string。
func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// asInt64 从 map 里安全取 int64。
func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}
