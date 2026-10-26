package dao

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/risk/domain"
)

// SyncAuditTaskInput 是 SyncAuditRecorder.Finish 落库时 task_input 的内容。
type SyncAuditTaskInput struct {
	BizIDs   []string `json:"biz_ids"`
	SubTypes []string `json:"sub_types"`
}

// SyncAuditRecorder 把一次业务流程里的多次同步风控调用合并成一条风控审计记录。
//
// 设计意图（设计精讲 §四）：一次成片 pre_create 可能要审多组文本，若每组都落一条记录
// 会刷爆记录表；Recorder 在内存中收集本次所有 (subProduct, bizID)，Finish 时写一条
// 汇总记录，用于审计对账。三种执行模式：
//
//	Run         单条文本/图片，一次风控调用
//	RunByTick   多条文本，ticker 限速派发（保护下游）
//	RunParallel 多条并发，聚合语义 err > !pass > pass
//
// 用法：
//
//	recorder := NewSyncAuditRecorder(ctx, userID, projectID, taskStepID, "risk:5:mix_video:precreate", backend, records)
//	passed, reason, err := recorder.Run(req, subProduct) // 调用一次或多次
//	// ... 其它风控调用 ...
//	recorder.Finish(passed, reason, err) // 最后统一落一条记录
//
// 图片派发/聚合策略（限速闸门 + 一票否决）收敛在 domain.SyncAuditImages /
// domain.AggregateImageResults，此处只负责审计元信息收集与落库。
type SyncAuditRecorder struct {
	ctx        context.Context
	userID     int64
	projectID  string
	taskStepID int64
	taskType   string   // 如 "risk:5:mix_video:precreate"
	subTypes   []string // 本次涉及的所有 subProduct
	bizIDs     []string // 本次涉及的所有 bizID
	backend    domain.AuditBackend
	records    domain.AuditRecordRepository

	mu sync.Mutex
}

// NewSyncAuditRecorder 创建 recorder。
func NewSyncAuditRecorder(ctx context.Context, userID int64, projectID string, taskStepID int64, taskType string, backend domain.AuditBackend, records domain.AuditRecordRepository) *SyncAuditRecorder {
	return &SyncAuditRecorder{
		ctx:        ctx,
		userID:     userID,
		projectID:  projectID,
		taskStepID: taskStepID,
		taskType:   taskType,
		backend:    backend,
		records:    records,
	}
}

// Run 执行一次同步风控并记录审计元信息（subProduct + bizID）。
func (r *SyncAuditRecorder) Run(req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	r.mu.Lock()
	r.subTypes = append(r.subTypes, subProduct)
	r.mu.Unlock()

	passed, reason, err := r.syncAudit(r.ctx, req, subProduct)

	r.mu.Lock()
	r.bizIDs = append(r.bizIDs, req.BizID)
	r.mu.Unlock()

	return passed, reason, err
}

// RunByTick 多条文本按 ticker 限速派发（防止把风控服务打挂），聚合语义同 RunParallel。
func (r *SyncAuditRecorder) RunByTick(reqs []*domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	if len(reqs) == 0 {
		return true, "", nil
	}
	r.mu.Lock()
	r.subTypes = append(r.subTypes, subProduct)
	r.mu.Unlock()

	// 与 domain.SyncAuditImages 同构的限速闸门：限派发速率、不限并发数
	results := make(chan domain.ImageAuditResult, len(reqs))
	var wg sync.WaitGroup
	ticker := time.NewTicker(domain.DefaultImageTicker)
	defer ticker.Stop()

	for _, req := range reqs {
		select {
		case <-ticker.C:
		case <-r.ctx.Done():
			return false, "", r.ctx.Err()
		}
		wg.Add(1)
		go func(req *domain.RiskSubmitRequest) {
			defer wg.Done()
			passed, reason, err := r.syncAudit(r.ctx, req, subProduct)
			r.mu.Lock()
			r.bizIDs = append(r.bizIDs, req.BizID)
			r.mu.Unlock()
			results <- domain.ImageAuditResult{Passed: passed, Reason: reason, Err: err}
		}(req)
	}
	wg.Wait()
	close(results)
	return domain.AggregateImageResults(results)
}

// RunParallel 多条并发执行，聚合语义 err > !pass > pass。
func (r *SyncAuditRecorder) RunParallel(reqs []*domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	if len(reqs) == 0 {
		return true, "", nil
	}
	r.mu.Lock()
	r.subTypes = append(r.subTypes, subProduct)
	r.mu.Unlock()

	results := make(chan domain.ImageAuditResult, len(reqs))
	var wg sync.WaitGroup
	for _, req := range reqs {
		wg.Add(1)
		go func(req *domain.RiskSubmitRequest) {
			defer wg.Done()
			passed, reason, err := r.syncAudit(r.ctx, req, subProduct)
			r.mu.Lock()
			r.bizIDs = append(r.bizIDs, req.BizID)
			r.mu.Unlock()
			results <- domain.ImageAuditResult{Passed: passed, Reason: reason, Err: err}
		}(req)
	}
	wg.Wait()
	close(results)
	return domain.AggregateImageResults(results)
}

// Finish 在业务逻辑所有风控调用完成后调用一次，写入一条汇总的风控审计记录。
//
// status：全部通过 → "done"；任一不通过或出错 → "fail"。
// task_input 记录本次涉及的所有 bizID + subProduct（审计对账用）。
func (r *SyncAuditRecorder) Finish(passed bool, reason string, err error) {
	taskInput, _ := json.Marshal(SyncAuditTaskInput{
		BizIDs:   r.bizIDs,
		SubTypes: r.subTypes,
	})
	status := domain.StatusDone
	if !passed || err != nil {
		status = domain.StatusFail
	}

	_, _ = r.records.Insert(r.ctx, &domain.AuditRecord{
		UserID:     r.userID,
		ProjectID:  r.projectID,
		TaskStepID: r.taskStepID,
		TaskType:   r.taskType,
		TaskInput:  string(taskInput),
		Status:     status,
	})
}

// syncAudit 文本/图片同步风控：阻塞等待结果，立即返回。
//
// 返回：
//   - passed=false + err=non-nil → 风控请求失败（系统错误）
//   - passed=false + reason=non-empty → 内容不通过（命中风控）
//   - passed=true → 通过
//
// 图片风控拆分为每次 1 张（domain.SyncAuditImages，限速派发 + 一票否决）；
// 文本风控单次请求直连 backend.Submit。
func (r *SyncAuditRecorder) syncAudit(ctx context.Context, req *domain.RiskSubmitRequest, subProduct string) (bool, string, error) {
	// 图片风控：拆分为每次 1 张，限速派发 + 一票否决
	if len(req.Content.Image) > 0 {
		return domain.SyncAuditImages(ctx, r.backend, req, subProduct)
	}
	// 文本风控：单次请求
	return r.backend.Submit(ctx, req, subProduct)
}
