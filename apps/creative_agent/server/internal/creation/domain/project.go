package domain

import "errors"

// Work state machine (four status states).
const (
	ProjectStatusDone    = "done"    // done (terminal, irreversible)
	ProjectStatusFail    = "fail"    // failed (terminal, irreversible)
	ProjectStatusRunning = "running" // generating
	ProjectStatusAudit   = "audit"   // under risk control (only for final-video async risk control; intermediate state)
)

// Points status (three points_status states).
const (
	PointStatusUnUsed = 0 // not deducted
	PointStatusUsed   = 1 // deducted
	PointStatusRefund = 2 // refunded
)

// Role types (decide which processing path to take).
const (
	RoleTypeDirector       = 1 // art director
	RoleTypePointOfView    = 2 // idea king (brainstorm)
	RoleTypeScreenplay     = 3 // gold scriptwriter
	RoleTypeVisualDesigner = 4 // visual designer (instant image)
	RoleTypeEditor         = 5 // edit master (mix-cut / instant video)
	RoleTypeDataAnalyst    = 6 // data analyst
)

// Creation types.
const (
	ProjectTypeMixVideo      = "mix_video"
	ProjectTypeMiaotuImage   = "miaotu"
	ProjectTypeMiaoyingVideo = "miaoying"
	ProjectTypeGraphicNotes  = "graphic_notes"
	ProjectTypeStoryPb       = "story_pb"
	ProjectTypeStoryDialogue = "story_dialogue"
)

// Retry types.
const (
	RetryTypeNone   = 0 // no retry
	RetryTypeFail   = 1 // failure retry (triggers another points deduction)
	RetryTypeResend = 2 // dissatisfaction retry
)

// Callback step: single / multi (passed through with the operator inputs; callbacks route by it).
const (
	CallbackStepOnly  = "only"
	CallbackStepMulti = "multi"
)

// Task types (task cards): the task_type values in responses.
const (
	TaskTypePushCard   = "push_card"
	TaskTypeNewSession = "new_session"
)

// Card types (task_arguments.card_type values; protocol strings, kept in Chinese).
const (
	CardTypeInputRisk         = "输入风控卡"
	CardTypeAuditPassed       = "审核通过卡"
	CardTypeProjectProcessing = "作品生产中卡"
	CardTypeInsufficientScore = "积分不足卡"
	CardTypeFailedRetry       = "失败重试卡"
	CardTypeRetryLimit        = "重试限制卡"
	CardTypeProjectFailed     = "作品生成失败卡"
	CardTypeOutputAudit       = "产出风控卡"
	CardTypeScriptResult      = "脚本结果卡"
	CardTypeVideoDelivery     = "成片交付卡"
	CardTypeImageDelivery     = "图片交付卡"
	CardTypeBrainstormResult  = "脑洞结果卡"
)

// Callback task statuses (the task_status values of task_detail callbacks).
const (
	CallbackTaskStatusSuccess    = "success"
	CallbackTaskStatusProcessing = "processing"
	CallbackTaskStatusFailed     = "failed"
)

// ErrProjectNotFound indicates no matching work was found.
var ErrProjectNotFound = errors.New("creation project not found")

// Project is the work aggregate root (cg_creation_project).
//
// It carries the state of points-bearing roles (final delivered works such as visual designer / edit master / instant image / instant video).
// One session maps to one project, reused throughout; no new sessions are created.
// State machine: running → audit → done / fail; points_status: 0 not deducted → 1 deducted → 2 refunded.
type Project struct {
	ID               uint64
	UserID           int64
	SessionID        string
	ProjectID        string
	ProjectType      string
	ProjectInput     string // JSON, user input parameters
	Status           string
	PointsStatus     int
	PointsCost       int
	PointsOrderID    string
	RetryNum         int
	FailReason       string
	Ext              string // JSON: TaskStep mapping, MsgID, etc.
	ProductionLineID string
	Progress         int
	Title            string
	CreateTime       int64
	UpdateTime       int64
}

// IsTerminal reports whether the state is terminal.
func (p *Project) IsTerminal() bool {
	return p.Status == ProjectStatusDone || p.Status == ProjectStatusFail
}

// MarkDone sets the success terminal state (idempotent: terminal states never change).
func (p *Project) MarkDone() bool {
	if p.IsTerminal() {
		return false
	}
	p.Status = ProjectStatusDone
	p.FailReason = ""
	return true
}

// MarkAudit sets the risk-control intermediate state (running → audit only).
func (p *Project) MarkAudit() bool {
	if p.Status != ProjectStatusRunning {
		return false
	}
	p.Status = ProjectStatusAudit
	return true
}

// MarkFail sets the failure terminal state and attempts a points refund. An external refund callback is required (crossing the points domain).
// Returns whether a state transition happened. refund is injected by the application layer (keeping the domain free of points dependencies).
// ⚠️ Note: when PointsOrderID != "", callers must pass a non-nil refund, otherwise points cannot be refunded (this is only a defensive fallback).
func (p *Project) MarkFail(reason string, refund func() error) (changed bool, err error) {
	if p.IsTerminal() {
		return false, nil
	}
	if p.PointsStatus == PointStatusUsed && p.PointsOrderID != "" {
		if refund == nil {
			return false, errors.New("creation: refund callback is nil while points need refund")
		}
		if rerr := refund(); rerr != nil {
			return false, rerr
		}
		p.PointsStatus = PointStatusRefund
	}
	p.Status = ProjectStatusFail
	p.FailReason = reason
	return true, nil
}

// MarkTimeout sets fail as the timeout fallback (running → fail + points refund only).
// ⚠️ Note: the audit state is deliberately excluded — audit belongs to the risk callback; the timeout job does not scan works under risk control.
func (p *Project) MarkTimeout(reason string, refund func() error) (bool, error) {
	if p.Status != ProjectStatusRunning {
		return false, nil
	}
	return p.MarkFail(reason, refund)
}
