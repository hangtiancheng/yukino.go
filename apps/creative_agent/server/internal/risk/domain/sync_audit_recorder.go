package domain

// SyncAuditRecorder 把一次业务流程里的多次同步风控调用合并成一条审计记录的接口。
//
// 依赖反转：接口定义在 risk.domain（共享值对象层，供跨 context 使用），
// 具体实现位于 risk.infrastructure（SyncAuditRecorder 结构体），
// creation.application 通过 RiskPort.NewSyncAuditRecorder 获取实现实例。
//
// 设计意图（设计精讲 §四）：一次成片 pre_create 可能要审多组文本，若每组都落一条记录
// 会刷爆记录表；Recorder 在内存中收集本次所有 (subProduct, bizID)，Finish 时写一条
// 汇总记录，用于审计对账。
//
// 用法：
//
//	recorder := riskPort.NewSyncAuditRecorder(ctx, userID, projectID, taskStepID, taskType)
//	passed, reason, err := recorder.Run(req, subProduct) // 调用一次或多次
//	// ... 其它风控调用 ...
//	recorder.Finish(passed, reason, err) // 最后统一落一条记录
type SyncAuditRecorder interface {
	// Run 执行一次同步风控并记录审计元信息（subProduct + bizID）。
	Run(req *RiskSubmitRequest, subProduct string) (passed bool, reason string, err error)
	// RunByTick 多条文本按 ticker 限速派发（防止把风控服务打挂），聚合语义 err > !pass > pass。
	RunByTick(reqs []*RiskSubmitRequest, subProduct string) (passed bool, reason string, err error)
	// RunParallel 多条并发执行，聚合语义 err > !pass > pass。
	RunParallel(reqs []*RiskSubmitRequest, subProduct string) (passed bool, reason string, err error)
	// Finish 在业务逻辑所有风控调用完成后调用一次，写入一条汇总的审计记录。
	// status：全部通过 → "done"；任一不通过或出错 → "fail"。
	Finish(passed bool, reason string, err error)
}
