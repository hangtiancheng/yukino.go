package domain

import "errors"

// 作品状态机（status 四态）。
const (
	ProjectStatusDone    = "done"    // 完成（终态，不可逆）
	ProjectStatusFail    = "fail"    // 失败（终态，不可逆）
	ProjectStatusRunning = "running" // 生成中
	ProjectStatusAudit   = "audit"   // 风控中（仅成片视频异步风控用，中间态）
)

// 积分状态（points_status 三态）。
const (
	PointStatusUnUsed = 0 // 未扣除
	PointStatusUsed   = 1 // 已扣除
	PointStatusRefund = 2 // 已退款
)

// 角色类型（决定走哪条处理路径）。
const (
	RoleTypeDirector       = 1 // 艺术总监
	RoleTypePointOfView    = 2 // 点子王（脑洞）
	RoleTypeScreenplay     = 3 // 金牌编剧
	RoleTypeVisualDesigner = 4 // 视觉设计师（妙图）
	RoleTypeEditor         = 5 // 剪辑大师（混剪/妙影）
	RoleTypeDataAnalyst    = 6 // 数据分析师
)

// 创作类型。
const (
	ProjectTypeMixVideo      = "mix_video"
	ProjectTypeMiaotuImage   = "miaotu"
	ProjectTypeMiaoyingVideo = "miaoying"
	ProjectTypeGraphicNotes  = "graphic_notes"
	ProjectTypeStoryPb       = "story_pb"
	ProjectTypeStoryDialogue = "story_dialogue"
)

// 重试类型。
const (
	RetryTypeNone   = 0 // 不重试
	RetryTypeFail   = 1 // 失败重试（会触发再扣一次积分）
	RetryTypeResend = 2 // 不满意重试
)

// 回调步骤：单步 / 多步（随算子入参透传，回调时据此路由）。
const (
	CallbackStepOnly  = "only"
	CallbackStepMulti = "multi"
)

// 任务类型（任务卡）：响应里 task_type 的取值。
const (
	TaskTypePushCard   = "push_card"
	TaskTypeNewSession = "new_session"
)

// 卡片类型（task_arguments.card_type 取值）。
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

// 回调任务状态（task_detail 回调的 task_status 取值）。
const (
	CallbackTaskStatusSuccess    = "success"
	CallbackTaskStatusProcessing = "processing"
	CallbackTaskStatusFailed     = "failed"
)

// ErrProjectNotFound 表示未找到对应作品。
var ErrProjectNotFound = errors.New("creation project not found")

// Project 作品聚合根（cg_creation_project）。
//
// 承载有积分角色的状态（视觉设计师/剪辑大师/妙图妙影等最终交付作品）。
// 一次 session 对应一个 project，全程复用、不做新会话。
// 状态机：running → audit → done / fail；points_status: 0 未扣 → 1 已扣 → 2 已退。
type Project struct {
	ID               uint64
	UserID           int64
	SessionID        string
	ProjectID        string
	ProjectType      string
	ProjectInput     string // JSON，用户输入参数
	Status           string
	PointsStatus     int
	PointsCost       int
	PointsOrderID    string
	RetryNum         int
	FailReason       string
	Ext              string // JSON: TaskStep 映射、MsgID 等
	ProductionLineID string
	Progress         int
	Title            string
	CreateTime       int64
	UpdateTime       int64
}

// IsTerminal 是否终态。
func (p *Project) IsTerminal() bool {
	return p.Status == ProjectStatusDone || p.Status == ProjectStatusFail
}

// MarkDone 置成功终态（幂等：终态不变）。
func (p *Project) MarkDone() bool {
	if p.IsTerminal() {
		return false
	}
	p.Status = ProjectStatusDone
	p.FailReason = ""
	return true
}

// MarkAudit 置风控中间态（仅 running → audit）。
func (p *Project) MarkAudit() bool {
	if p.Status != ProjectStatusRunning {
		return false
	}
	p.Status = ProjectStatusAudit
	return true
}

// MarkFail 置失败终态并尝试退积分。需外部提供退积分回调（跨 points 域）。
// 返回是否发生状态流转。refund 由 application 层注入（保持领域不依赖 points）。
// ⚠️ 注意: 当 PointsOrderID != "" 时调用方必须传入非 nil 的 refund，否则积分无法退还（此处仅防御性兜底）。
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

// MarkTimeout 超时兜底置 fail（仅 running → fail + 退积分）。
// ⚠️ 注意: 刻意排除 audit 态——audit 归风控回调管，超时 job 不扫风控中作品。
func (p *Project) MarkTimeout(reason string, refund func() error) (bool, error) {
	if p.Status != ProjectStatusRunning {
		return false, nil
	}
	return p.MarkFail(reason, refund)
}
