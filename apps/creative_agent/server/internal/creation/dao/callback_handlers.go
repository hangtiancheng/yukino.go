package dao

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	riskmodel "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// HandleMixVideoScriptCallback 编剧(role=3)脚本回调：段落文本风控 → record=done + 写剧本。
//
// 对齐 §4.3：编剧阶段产出脚本段落文本，做同步风控，通过则 record=done 并把剧本
// 写进 ext.step_output["3:mix_video"]（成片 pre_create 从这里取剧本做输入）。
// 编剧无积分：命中风控只置 record=fail，不退积分。
//
// 幂等闸门（对齐 spec §5.1）：处理前用 records.MarkStatusIf 做数据层乐观锁，
// 只有 running 才流转——done/fail 直接短路，防并发重复回调。
func HandleMixVideoScriptCallback(ctx context.Context, d *CallbackDeps, req *contracts.CallbackReq) error {
	passThrough, err := contracts.ParseExtendParams(req.ExtendParams)
	if err != nil {
		return err
	}
	recordID := uint64(passThrough.CallbackRecordID)

	// 终态短路（防重复回调）：record 已是 done/fail 直接返回。
	// ⚠️ 注意：record 可能不存在（首次 pre_create 未落记录），此时 recordID=0，
	// 不做乐观锁判断（旧实现同样只在有 record 时短路）。
	if recordID > 0 {
		record, err := d.records.Get(ctx, recordID)
		if err == nil && (record.Status == domain.ProjectStatusDone || record.Status == domain.ProjectStatusFail) {
			return nil // 已完成，不重复处理
		}
	}

	switch req.TaskStatus {
	case domain.CallbackTaskStatusFailed:
		// 脚本生成失败 → record=fail（编剧无积分，不退）
		// ⚠️ 并发保护：updateRecordStatus 返回 false 说明已被并发回调流转到终态
		//（如 success→done），不得覆盖成功结果，幂等短路返回。
		if !updateRecordStatus(ctx, d.records, passThrough, domain.ProjectStatusFail) {
			return nil // 已被其它回调流转（并发重入 / 重复回调），幂等短路
		}
		return nil

	case domain.CallbackTaskStatusSuccess:
		// 1. 脚本段落文本风控（同步）——复用阶段 3 的 SyncAuditRecorder
		scriptText := extractScriptText(req.SpecialInfo)
		riskReq := riskmodel.NewRiskSubmitRequest(req.UserID, req.ProjectID,
			fmt.Sprintf("script-%d", req.TaskID), scriptText)
		recorder := d.risk.NewSyncAuditRecorder(ctx, req.UserID, req.ProjectID, req.TaskID,
			"risk:mix_video:script")
		passed, reason, err := recorder.Run(riskReq, riskmodel.MixVideoT2IOutputSubproduct)
		recorder.Finish(passed, reason, err)

		if err != nil || !passed {
			// 命中 → record=fail + 输出风控卡（不退积分，编剧无积分）
			// ⚠️ 并发保护：与 failed 路径一致，尊重 MarkStatusIf CAS 结果，
			// 已被并发流转到终态时不覆盖。
			if !updateRecordStatus(ctx, d.records, passThrough, domain.ProjectStatusFail) {
				return nil // 已被其它回调流转（并发重入 / 重复回调），幂等短路
			}
			return nil
		}

		// 2. 通过 → record=done + 把脚本写进 ext.step_output["3:mix_video"]。
		//    数据层乐观锁：仅 running → done 原子流转，done/fail 直接短路（防并发重复回调）。
		if recordID > 0 {
			if done, _ := d.records.MarkStatusIf(ctx, recordID, domain.ProjectStatusDone, domain.ProjectStatusRunning); !done {
				return nil // 已被其它回调流转（并发重入 / 重复回调），幂等短路
			}
		}
		saveScreenplayToExt(ctx, d.projects, req, scriptText)
		return nil
	}
	return nil
}

// HandleMixVideoProjectCallback 成片(role=5)回调：audit 中间态核心。
//
// 对齐 §4.4：成片产出成功后**不能直接 done**，要先置 audit，等视频异步风控回调
// 后才能定终态（决策 1）。
//
// 流程：processing 更新进度 → failed 退积分+fail → success 先置 audit（乐观锁
// running→audit）→ 产出文本风控 → 视频异步风控（AsyncAudit）→ recordID 透传。
//
// 幂等闸门（对齐 spec §5.1）：查出的 project 终态短路 + audit 置位用乐观锁，
// 防并发重复回调。
func HandleMixVideoProjectCallback(ctx context.Context, d *CallbackDeps, req *contracts.CallbackReq) error {
	// 查作品（project_id 关联）
	project, err := d.projects.GetByProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}

	// ⚠️ 终态短路：防止算子重复回调（决策 1：终态不可逆）
	if project.Status == domain.ProjectStatusDone || project.Status == domain.ProjectStatusFail {
		return nil
	}

	switch req.TaskStatus {
	case domain.CallbackTaskStatusProcessing:
		// 进度更新，保持 running，更新 progress
		project.Progress = req.ProgressPercentage
		_ = d.projects.Update(ctx, project)
		return nil

	case domain.CallbackTaskStatusFailed:
		// 直接失败：退积分 + status=fail
		return failProjectCallback(ctx, d, project, contracts.BuildFailReason("任务失败", req.FailReason))

	case domain.CallbackTaskStatusSuccess:
		// ⚠️ 核心逻辑：先置 audit，再送风控（不是 done！）
		//    对用户表现为"审核中"，等风控回调定终态。
		//
		//    原子性（防 audit→done 丢失更新竞态）：MarkAudit 置位与
		//    saveAsyncAuditRecordID 透传原本是两次独立全字段 Update，risk 回调
		//    若在两次写之间到达会读到 audit → 落定 done，随后本 handler 的陈旧的
		//    第二次 Update 会把 done 覆盖回 audit（永久卡住）。修复：先提交异步风控
		//    拿到 recordID，再一次性用 CAS（UpdateIfStatus WHERE status='running'）
		//    写入 status=audit + ext.async_audit_record_id，消除中间窗口；CAS 失败
		//    表示状态已被其它回调推进（如 risk 回调已落定），幂等短路不覆盖。
		if project.Status != domain.ProjectStatusRunning {
			return nil // 非 running（并发重入 / 已流转），幂等短路
		}

		// 1. 产出标题/正文文本风控（同步）
		passed, reason, err := syncAuditOutputText(ctx, d, project, req.SpecialInfo)
		if err != nil || !passed {
			// 命中 → 退积分 + fail + 输出风控卡
			return failProjectCallback(ctx, d, project, contracts.BuildFailReason("产出风控拦截", reason))
		}

		// 2. 产出视频风控（异步）——视频审核耗时长，提交即返回，靠回调闭环
		videoReq := riskmodel.NewRiskSubmitRequest(project.UserID, project.ProjectID,
			fmt.Sprintf("%d", req.TaskID), "")
		videoReq.Content.Text = extractVideoDescription(req.SpecialInfo)

		recordID, err := d.risk.AsyncAudit(ctx, videoReq, riskmodel.MixVideo60sT2VOutputSubproduct, buildRiskCallbackURL())
		if err != nil {
			// 异步提交失败 → 返回错误，Dispatch 写补偿表（阶段 5 重试）
			return err
		}

		// 3. 一次 CAS 写入 status=audit + ext.async_audit_record_id（running → audit）
		//    仅当 DB 中 status 仍是 running 才成功；否则 risk 回调已落定终态，短路。
		project.MarkAudit()
		setAsyncAuditRecordID(project, recordID)
		if ok, err := d.projects.UpdateIfStatus(ctx, project, domain.ProjectStatusRunning); err != nil {
			return err
		} else if !ok {
			return nil // 已被其它回调流转（risk 回调已落定），幂等短路不覆盖终态
		}
		return nil
	}
	return nil
}

// updateRecordStatus 更新回调记录状态（数据层乐观锁：仅 running 流转），返回是否流转成功。
//
// 对齐 spec §5.1：用 MarkStatusIf 做乐观锁幂等闸门——只有 running 才流转，
// done/fail 直接短路，防并发重复回调。返回 false 表示已被其它回调流转到终态
// （如并发 success→done），调用方不得覆盖终态、应幂等短路。
// recordID<=0 时无记录可更新（旧实现同语义），返回 false。
func updateRecordStatus(ctx context.Context, records domain.CallbackRecordRepository,
	passThrough *contracts.ExtendParams, status string) bool {
	if passThrough.CallbackRecordID <= 0 {
		return false
	}
	ok, _ := records.MarkStatusIf(ctx, uint64(passThrough.CallbackRecordID), status, domain.ProjectStatusRunning)
	return ok
}

// syncAuditOutputText 产出文本风控（同步），复用阶段 3 的 SyncAudit 端口。
func syncAuditOutputText(ctx context.Context, d *CallbackDeps, project *domain.Project, specialInfo json.RawMessage) (bool, string, error) {
	text := extractScriptText(specialInfo)
	if text == "" {
		// 无产出文本（可能只有视频）→ 通过
		return true, "", nil
	}
	req := riskmodel.NewRiskSubmitRequest(project.UserID, project.ProjectID, "output_text", text)
	return d.risk.SyncAudit(ctx, req, riskmodel.MixVideoTitleT2TOutputSubproduct)
}

// failProjectCallback 回调失败路径：退积分 + 置 fail + 落库。
//
// 幂等：project 终态短路由调用方完成；MarkFail 内部处理"仅 points_status=used 才退"
// 不变量（points.Refund 本身幂等，已退过返回 nil）。返回 nil 表示已处理。
func failProjectCallback(ctx context.Context, d *CallbackDeps, project *domain.Project, reason string) error {
	changed, err := project.MarkFail(reason, func() error {
		return d.points.Refund(ctx, project.PointsOrderID, project.UserID, project.ProjectType)
	})
	if err != nil || !changed {
		return err
	}
	return d.projects.Update(ctx, project)
}

// saveScreenplayToExt 把剧本写进 ext.step_output["3:mix_video"]（成片阶段输入从这里取）。
func saveScreenplayToExt(ctx context.Context, projects domain.ProjectRepository, req *contracts.CallbackReq, scriptText string) {
	project, err := projects.GetByProjectID(ctx, req.ProjectID)
	if err != nil {
		return
	}
	ext, _ := domain.ParseExt(project.Ext)
	ext.SetStepOutput(domain.RoleTypeScreenplay, domain.ProjectTypeMixVideo, scriptText)
	extJSON, _ := ext.ToJSON()
	project.Ext = extJSON
	_ = projects.Update(ctx, project)
}

// setAsyncAuditRecordID 把异步风控 recordID 写入 project 内存的 ext（不落库）。
//
// ⚠️ 与旧 saveAsyncAuditRecordID 不同：这里只改内存，落库由调用方的 CAS
// （UpdateIfStatus WHERE status='running'）一次写入 status=audit + ext，
// 避免"置 audit"与"透传 recordID"两次独立 Update 之间的丢失更新竞态。
func setAsyncAuditRecordID(project *domain.Project, recordID int64) {
	ext, _ := domain.ParseExt(project.Ext)
	if ext.AsyncAuditRecordID == 0 {
		ext.AsyncAuditRecordID = recordID
		extJSON, _ := ext.ToJSON()
		project.Ext = extJSON
	}
}

// extractScriptText 从回调 special_info 提取剧本文本。
// 本地复现：special_info 是 JSON（或 base64(JSON)），取 script/text 字段。
func extractScriptText(specialInfo json.RawMessage) string {
	raw := string(specialInfo)
	if raw == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err == nil {
		if s, ok := payload["script"].(string); ok {
			return s
		}
		if s, ok := payload["text"].(string); ok {
			return s
		}
	}
	return raw // 兜底：原文作为待审文本
}

// extractVideoDescription 从 special_info 提取视频描述（供异步风控判审）。
func extractVideoDescription(specialInfo json.RawMessage) string {
	raw := string(specialInfo)
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err == nil {
		if s, ok := payload["description"].(string); ok {
			return s
		}
	}
	return ""
}

// buildRiskCallbackURL 构造风控回调地址（异步风控提交时随请求带过去）。
func buildRiskCallbackURL() string {
	return "/inner/callback/risk"
}
