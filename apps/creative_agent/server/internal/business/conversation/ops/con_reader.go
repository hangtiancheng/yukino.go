package ops

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"
)

// con_reader 写入 flowcontext 的 key 常量。
const (
	FCKRequest     = "request"
	FCKServerMsgID = "serverMsgId"
	FCKUserID      = "userId"
	FCKSessionID   = "session_id"
	FCKAgentID     = "agent_id"
	FCKQuery       = "query"
	FCKGdpCtx      = "gdpCtx"

	// Phase 4: 快捷路径参数
	FCKRequestType = "requestType"
	FCKResume      = "resume"
	// FCKOriginMsgID 点卡确认后前端回传的上一步消息 ID（组织上下文用）。
	// 由前端从 submit_clarification 响应拿到 originMsgId，下一轮请求带回，
	// fastpath 用它追溯前序脚本（对齐真实系统 OriginMsgID 链路）。
	FCKOriginMsgID = "origin_msg_id"
)

// ConversationRequest 是 con_reader 解析的 HTTP JSON 请求体。
type ConversationRequest struct {
	UserID    int64  `json:"userId"`
	SessionID string `json:"sessionId"`
	AgentID   string `json:"agentId"`
	Query     string `json:"query"`
	// Reconnect 断线重连回放参数（前端在 EventSource 断线后携带）
	Reconnect bool   `json:"reconnect"`
	MsgID     string `json:"msgId"`
	LastSeq   int64  `json:"lastSeq"`

	// Phase 4: 快捷路径参数
	RequestType string `json:"requestType"` // "" / "retry" / "cron"
	Resume      bool   `json:"resume"`      // HIL 断点恢复
	// OriginMsgID 点卡确认后前端回传的上一步消息 ID（fastpath 追溯前序脚本用）。
	OriginMsgID string `json:"originMsgId"`
}

// ConversationLoadResult 保存 con_reader 在 LoadInput 阶段产出的数据。
type ConversationLoadResult struct {
	Request ConversationRequest
}

// ConReaderNode 是 conversation flow 的入口算子。
//
// 职责：解析 HTTP 请求 → 查/建 session 和 message_record → (异步分流) → 写 flowcontext。
//
// Phase 2：当 asyncEnabled=true 时，tryEnqueueAsync 会将请求发送到 Redis Stream
// 由后台 Worker 异步执行；任何步骤失败都静默回退同步路径。
type ConReaderNode struct {
	sessionRepo       repository.SessionRepository       // cg_session DAO（查/建会话）
	messageRecordRepo repository.MessageRecordRepository // cg_message_record DAO（建消息记录）
	asyncRepo         repository.AsyncRequestRepository  // cg_async_request DAO（异步请求落库，nil 时异步禁用）
	streamQ           infra.StreamQueue                  // 消息队列（XADD 投递异步任务，nil 时异步禁用）
	locker            infra.Locker                       // 幂等锁（SETNX；nil 时异步禁用）
	asyncEnabled      bool                               // 异步开关，由 ASYNC_ENABLED 环境变量控制

	In ConversationLoadResult
}

// NewConReaderNode 创建 ConReaderNode。
func NewConReaderNode(
	sessionRepo repository.SessionRepository,
	msgRepo repository.MessageRecordRepository,
	asyncRepo repository.AsyncRequestRepository,
	streamQ infra.StreamQueue,
	locker infra.Locker,
	asyncEnabled bool,
) *ConReaderNode {
	return &ConReaderNode{
		sessionRepo:       sessionRepo,
		messageRecordRepo: msgRepo,
		asyncRepo:         asyncRepo,
		streamQ:           streamQ,
		locker:            locker,
		asyncEnabled:      asyncEnabled,
	}
}

// Name 返回算子名（flow 配置里引用用）。
func (op *ConReaderNode) Name() string { return "con_reader" }

// DoImpl 实现四阶段算子骨架：LoadInput → Call → (tryEnqueueAsync) → WriteOutput。
func (op *ConReaderNode) DoImpl(ctx *seda.Context) (int, string) {
	httpBody, ok := ctx.Get(FCKGdpCtx).([]byte)
	if !ok {
		return -1, "con_reader: 上下文中没有 HTTP body"
	}
	if err := op.loadInput(httpBody); err != nil {
		return -1, fmt.Sprintf("con_reader: 解析错误: %v", err)
	}

	// 从 flow context 恢复 HTTP span，包 con_reader.* span。
	// 后续 session/message_record 的 SQL、Redis SetNX、XADD traceparent 全部挂进 HTTP trace。
	gctx := tracex.ContextFromFlow(ctx)
	gctx, span := otel.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/operators").Start(gctx, "con_reader.process")
	defer span.End()
	span.SetAttributes(attribute.String("session_id", op.In.Request.SessionID))

	if err := op.call(gctx, ctx); err != nil {
		return -1, fmt.Sprintf("con_reader: %v", err)
	}

	// Phase 2.5: 断线重连 → 复用已有记录 → 强制走 attach 回放
	if req := op.In.Request; req.Reconnect && req.MsgID != "" {
		if _, ok := ctx.Get("_reconnectMsgID").(string); ok {
			ctx.Set("asyncShortCircuit", true)
		}
	} else if op.asyncEnabled && op.asyncRepo != nil && op.streamQ != nil {
		op.tryEnqueueAsync(gctx, ctx)
	}

	op.writeOutput(ctx)
	return 0, ""
}

func (op *ConReaderNode) loadInput(body []byte) error {
	var req ConversationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return fmt.Errorf("非法 JSON: %w", err)
	}
	// 断线重连回放不携带 query（前端 openReplay 只带 msgId+lastSeq），跳过必填校验。
	if req.Query == "" && !(req.Reconnect && req.MsgID != "") {
		return fmt.Errorf("query 是必填字段")
	}
	if req.SessionID == "" {
		return fmt.Errorf("sessionId 是必填字段")
	}
	if req.AgentID == "" {
		return fmt.Errorf("agentId 是必填字段")
	}
	if req.UserID == 0 {
		return fmt.Errorf("userId 是必填字段")
	}
	op.In.Request = req
	return nil
}

func (op *ConReaderNode) call(gctx context.Context, ctx *seda.Context) error {
	req := op.In.Request

	// 1. 断线重连：复用前端指定的已有记录，跳过建账
	if req.Reconnect && req.MsgID != "" {
		existing, err := op.messageRecordRepo.GetByMessageID(gctx, req.MsgID)
		if err == nil && existing != nil {
			ctx.Set("_messageID", existing.MessageID)
			ctx.Set("_reconnectMsgID", existing.MessageID)
			ctx.Set("_lastSeq", req.LastSeq)
			return nil
		}
		// 记录不存在 → 回退同步新建（普通请求）
	}

	// 2. 确定 message_id：每次请求都新建（雪花 ID）。
	//    不复用"最近一条 InProgress"记录——那会让同一会话连发的两条消息
	//    串到同一个 msgID（串账）。断线续传由前端显式带 msgId 定位（见 reconnect）。
	msgID := model.NextIDString()

	// 3. 查或建 session
	_, err := op.sessionRepo.GetBySessionID(gctx, req.SessionID)
	if errors.Is(err, repository.ErrSessionNotFound) {
		sess := &model.CGSession{
			SessionID:   req.SessionID,
			UserID:      req.UserID,
			AgentID:     req.AgentID,
			Status:      model.SessionStatusInit,
			Pd:          "mini",
			SessionName: "默认会话",
		}
		if err := op.sessionRepo.Insert(gctx, sess); err != nil {
			return fmt.Errorf("创建 session 失败: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("查询 session 失败: %w", err)
	}

	// 3. 新建 message_record
	ct := model.NewChatText(req.Query)
	ctJSON, err := json.Marshal(ct)
	if err != nil {
		return fmt.Errorf("序列化 chat_text 失败: %w", err)
	}
	rec := &model.CGMessageRecord{
		MessageID: msgID,
		UserID:    req.UserID,
		AgentID:   req.AgentID,
		SessionID: req.SessionID,
		ChatText:  string(ctJSON),
		Status:    model.MessageStatusInProgress,
		Pd:        "mini",
	}
	// 写入 trace_id 到 log_id，便于 DB 反查链路。
	if sc := trace.SpanContextFromContext(gctx); sc.IsValid() {
		rec.LogID = sc.TraceID().String()
	}
	if err := op.messageRecordRepo.Insert(gctx, rec); err != nil {
		return fmt.Errorf("创建 message_record 失败: %w", err)
	}

	ctx.Set("_messageID", msgID)
	return nil
}

func (op *ConReaderNode) writeOutput(ctx *seda.Context) {
	req := op.In.Request
	msgID, _ := ctx.Get("_messageID").(string)

	ctx.Set(FCKRequest, req)
	ctx.Set(FCKServerMsgID, msgID)
	ctx.Set(FCKUserID, req.UserID)
	ctx.Set(FCKSessionID, req.SessionID)
	ctx.Set(FCKAgentID, req.AgentID)
	ctx.Set(FCKQuery, req.Query)

	// Phase 4: 快捷路径参数透传
	ctx.Set(FCKRequestType, req.RequestType)
	ctx.Set(FCKResume, req.Resume)
	ctx.Set(FCKOriginMsgID, req.OriginMsgID)

	// Phase 2.5: 断线重连透传
	if rID, ok := ctx.Get("_reconnectMsgID").(string); ok && rID != "" {
		ctx.Set("reconnectMsgId", rID)
	}
	if ls, ok := ctx.Get("_lastSeq").(int64); ok {
		ctx.Set("lastSeq", ls)
	}
}

// tryEnqueueAsync 尝试将请求异步化：幂等锁 → 写库 → 推 stream。
// 任何一步失败都静默回退同步路径（不阻断请求）。
// gctx 带 flow context 恢复的 HTTP span：SetNX/Insert/XADD 全部挂进 HTTP trace。
func (op *ConReaderNode) tryEnqueueAsync(gctx context.Context, ctx *seda.Context) {
	req := op.In.Request
	msgID, _ := ctx.Get("_messageID").(string)

	// 1. 5s 时间窗幂等：防用户快速重复点击
	dedupKey := computeDedupKey(req.SessionID, msgID, req.Query)
	if op.locker == nil {
		return // 锁不可用 → 回退同步
	}
	acquired, err := op.locker.SetNX(gctx, dedupKey, msgID, 5*time.Second)
	if err != nil || !acquired {
		return // 幂等冲突或锁不可用 → 回退同步
	}

	// 2. INSERT cg_async_request (Status=Pending)
	asyncReqID := model.NextIDString()
	asyncReq := &model.CGAsyncRequest{
		AsyncRequestID: asyncReqID,
		MessageID:      msgID,
		UserID:         req.UserID,
		SessionID:      req.SessionID,
		AgentID:        req.AgentID,
		Query:          req.Query,
		DedupKey:       dedupKey,
	}
	// 写入 trace_id 到 log_id，便于 DB 反查链路。
	if tid := tracex.TraceIDFromFlow(ctx); tid != "" {
		asyncReq.LogID = tid
	}
	if err := op.asyncRepo.InsertPending(gctx, asyncReq); err != nil {
		_ = op.locker.Del(gctx, dedupKey) // 释放幂等锁
		return
	}

	// 3. XADD stream — Worker will pick up
	body, _ := json.Marshal(map[string]interface{}{
		"async_request_id": asyncReqID,
		"msg_id":           msgID,
		"user_id":          req.UserID,
		"session_id":       req.SessionID,
		"agent_id":         req.AgentID,
		"query":            req.Query,
		"requestType":      req.RequestType,
		"resume":           req.Resume,
		"originMsgId":      req.OriginMsgID,
		"traceparent":      tracex.InjectCarrier(gctx)["traceparent"],
	})
	if err := op.streamQ.Push(gctx, msgID, body); err != nil {
		return // DB 已落，recovery 兜底
	}

	// 4. 成功：标记异步短路
	ctx.Set("asyncShortCircuit", true)
	ctx.Set("asyncRequestId", asyncReqID)
}

// computeDedupKey 根据 session+msgID+query 生成 5s 窗口的幂等去重 key。
func computeDedupKey(sessionID, msgID, query string) string {
	raw := sessionID + "|" + msgID + "|" + query
	sum := md5.Sum([]byte(raw))
	return "cg:dedup:" + hex.EncodeToString(sum[:])
}
