package domain

// SyncAuditRecorder is the interface merging multiple sync risk-control calls within one business flow into a single audit record.
//
// Dependency inversion: the interface is defined in risk.domain (the shared value-object layer, for cross-context use);
// the concrete implementation lives in risk.infrastructure (the SyncAuditRecorder struct);
// creation.application obtains an instance via RiskPort.NewSyncAuditRecorder.
//
// Design intent (design deep-dive §4): one final-video pre_create may audit several text groups; writing one record per group
// would flood the record table. The Recorder collects all (subProduct, bizID) pairs of this round in memory and Finish writes one
// summary record for audit reconciliation.
//
// Usage:
//
//	recorder := riskPort.NewSyncAuditRecorder(ctx, userID, projectID, taskStepID, taskType)
//	passed, reason, err := recorder.Run(req, subProduct) // called once or more
//	// ... other risk-control calls ...
//	recorder.Finish(passed, reason, err) // finally write one summary record
type SyncAuditRecorder interface {
	// Run executes one sync risk-control call and records the audit metadata (subProduct + bizID).
	Run(req *RiskSubmitRequest, subProduct string) (passed bool, reason string, err error)
	// RunByTick dispatches multiple texts with ticker rate limiting (protecting the risk service), aggregating with err > !pass > pass.
	RunByTick(reqs []*RiskSubmitRequest, subProduct string) (passed bool, reason string, err error)
	// RunParallel executes multiple texts concurrently, aggregating with err > !pass > pass.
	RunParallel(reqs []*RiskSubmitRequest, subProduct string) (passed bool, reason string, err error)
	// Finish is called once after all risk-control calls of the business logic complete, writing one summary audit record.
	// status: all passed → "done"; any rejection or error → "fail".
	Finish(passed bool, reason string, err error)
}
