package dao

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/contracts"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/service"
)

// CallbackHandler 是 task_detail 回调的处理器签名（按 task_type 注册）。
// handler 需自行用 records.MarkStatusIf 做数据层乐观锁幂等闸门——
// 只有 running 才流转，done/fail 直接短路，防并发重复回调。
type CallbackHandler func(ctx context.Context, d *CallbackDeps, req *contracts.CallbackReq) error

// CallbackDeps 是回调 handler 所需的跨域依赖（repos + 端口）。
//
// 回调分发器在 infrastructure 层，handler 内部协调 creation 仓储与 risk/points
// 跨域端口（service.RiskPort / service.PointsPort），不直接触碰
// risk/points 的基础设施实现——保持依赖单向（creation → risk/points）。
type CallbackDeps struct {
	projects domain.ProjectRepository
	records  domain.CallbackRecordRepository
	comp     domain.CompensationRepository
	risk     service.RiskPort
	points   service.PointsPort
}

// NewCallbackDeps 创建回调 handler 依赖集合（装配时由 dispatcher 构造注入）。
func NewCallbackDeps(projects domain.ProjectRepository, records domain.CallbackRecordRepository,
	comp domain.CompensationRepository, risk service.RiskPort, points service.PointsPort) *CallbackDeps {
	return &CallbackDeps{
		projects: projects,
		records:  records,
		comp:     comp,
		risk:     risk,
		points:   points,
	}
}

// CallbackDispatcher 任务回调分发器：解析 task_type → 异步执行 handler。
//
// 幂等闸门（对齐 spec §5.1）：handler 处理前用 CallbackRecordRepository.MarkStatusIf
// 做数据层乐观锁，只有 running 才流转（done/fail 直接短路），防并发重复回调。
//
// ⚠️ 核心设计（§4.2）：HTTP 立即返回 200，handler 在 goroutine 中异步执行——
// 防止算子因等待业务逻辑而超时重试。
type CallbackDispatcher struct {
	handlers map[string]CallbackHandler
	deps     *CallbackDeps
}

// NewCallbackDispatcher 创建回调分发器，注册所有 task_type → handler。
func NewCallbackDispatcher(deps *CallbackDeps) *CallbackDispatcher {
	d := &CallbackDispatcher{
		handlers: make(map[string]CallbackHandler),
		deps:     deps,
	}
	d.registerHandlers()
	return d
}

// registerHandlers 注册回调 handler（按 task_type 路由）。
func (d *CallbackDispatcher) registerHandlers() {
	d.handlers[domain.TaskTypeKey(domain.RoleTypeScreenplay, domain.ProjectTypeMixVideo)] = HandleMixVideoScriptCallback
	d.handlers[domain.TaskTypeKey(domain.RoleTypeEditor, domain.ProjectTypeMixVideo)] = HandleMixVideoProjectCallback
}

// Dispatch 分发一次任务回调（/inner/callback/task_detail 入口）。
//
// 流程：
//  1. 解析 extend_params，拿 task_type + callback_record_id
//  2. 更新 callback_record（记录回调原始产出 task_output，**不改 status**——
//     状态流转由各 handler 负责（done/fail），避免把终态覆盖成 success 破坏短路）
//  3. 注册表找 handler；找不到返回 FailCallbackResp
//  4. goroutine 异步执行 handler（context.WithoutCancel）；失败写补偿表
//     （BizTypeCallbackAgent，阶段 5 的补偿 job 重试）
func (d *CallbackDispatcher) Dispatch(ctx context.Context, req *contracts.CallbackReq) *contracts.CallbackResp {
	// 0. 从 extend_params 解析 task_type
	passThrough, err := contracts.ParseExtendParams(req.ExtendParams)
	if err != nil {
		return contracts.FailCallbackResp("解析 extend_params 失败: " + err.Error())
	}
	taskType := passThrough.TaskType

	// 1. 记录回调原始产出（task_output + 回调时间），但**不改 status**
	if passThrough.CallbackRecordID > 0 {
		_ = d.deps.records.Update(ctx, uint64(passThrough.CallbackRecordID), map[string]any{
			"task_output": string(req.SpecialInfo),
		})
	}

	// 2. 找 handler
	handler, ok := d.handlers[taskType]
	if !ok {
		return contracts.FailCallbackResp("未知 task_type: " + taskType)
	}

	// 3. ⚠️ 关键：HTTP 立即返回 200，handler 在 goroutine 中异步执行
	//    防止算子因等待而重试
	go func() {
		if err := handler(context.WithoutCancel(ctx), d.deps, req); err != nil {
			// 失败 → 写补偿表（阶段 5 的补偿 job 重试）
			d.insertCallbackAgentCompensation(context.Background(), req, taskType)
		}
	}()

	return contracts.SuccessCallbackResp()
}

// RetryCallbackAgent 供补偿 job 调用的回调 agent 重试入口。
//
// payload 是 insertCallbackAgentCompensation 写入的 JSON，包含：
//   - task_id / project_id / user_id / task_status / task_type
//
// 重试语义：重新走回调分发。handler 内部有数据层乐观锁幂等闸门（MarkStatusIf：
// done/fail 直接短路），且 Dispatch 只写回调原始产出不改终态——重复重试天然幂等，
// 不会二次退款。返回 error 表示分发失败（未知 task_type / extend_params 解析失败等），
// 由补偿 job 累加重试次数。
func (d *CallbackDispatcher) RetryCallbackAgent(ctx context.Context, payload map[string]any) error {
	req := &contracts.CallbackReq{
		TaskID:     asInt64(payload["task_id"]),
		ProjectID:  asString(payload["project_id"]),
		UserID:     asInt64(payload["user_id"]),
		TaskStatus: asString(payload["task_status"]),
	}
	if req.ProjectID == "" || req.TaskStatus == "" {
		return fmt.Errorf("retry callback agent: missing project_id/task_status")
	}
	// 补偿时没有原始 extend_params，用 payload 里存的 task_type 构造
	taskType := asString(payload["task_type"])
	if taskType == "" {
		return fmt.Errorf("retry callback agent: missing task_type")
	}
	req.ExtendParams = contracts.BuildExtendParamsJSON(taskType, asInt64(payload["callback_record_id"]),
		domain.CallbackStepMulti, 0, 0)
	// 同步重放（不走 goroutine）：分发失败返回 err；handler 成功返回 nil
	return d.redispatchCallback(ctx, req)
}

// redispatchCallback 同步执行一次回调分发（补偿 job 重试用，不走 goroutine）。
// 返回 err 表示分发失败（未知 task_type 等）；handler 内部错误同样以 err 返回。
func (d *CallbackDispatcher) redispatchCallback(ctx context.Context, req *contracts.CallbackReq) error {
	passThrough, err := contracts.ParseExtendParams(req.ExtendParams)
	if err != nil {
		return err
	}
	handler, ok := d.handlers[passThrough.TaskType]
	if !ok {
		return fmt.Errorf("未知 task_type: %s", passThrough.TaskType)
	}
	if passThrough.CallbackRecordID > 0 {
		_ = d.deps.records.Update(ctx, uint64(passThrough.CallbackRecordID), map[string]any{
			"task_output": string(req.SpecialInfo),
		})
	}
	return handler(context.WithoutCancel(ctx), d.deps, req)
}

// insertCallbackAgentCompensation 回调 handler 失败写补偿表（BizTypeCallbackAgent）。
// payload 存足重试所需信息（含 callback_record_id，供补偿 job 重构 extend_params 重放）。
func (d *CallbackDispatcher) insertCallbackAgentCompensation(ctx context.Context, req *contracts.CallbackReq, taskType string) {
	recordID := int64(0)
	if pt, err := contracts.ParseExtendParams(req.ExtendParams); err == nil {
		recordID = pt.CallbackRecordID
	}
	payload, _ := json.Marshal(map[string]any{
		"task_id":            req.TaskID,
		"project_id":         req.ProjectID,
		"user_id":            req.UserID,
		"task_status":        req.TaskStatus,
		"task_type":          taskType,
		"callback_record_id": recordID,
	})
	_, _ = d.deps.comp.Insert(ctx, &domain.Compensation{
		UserID:    req.UserID,
		ProjectID: req.ProjectID,
		BizID:     fmt.Sprintf("%d", req.TaskID),
		BizType:   domain.BizTypeCallbackAgent,
		Payload:   string(payload),
	})
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
