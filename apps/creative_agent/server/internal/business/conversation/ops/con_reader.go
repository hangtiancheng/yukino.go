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

// Key constants written to the flow context by con_reader.
const (
	FCKRequest     = "request"
	FCKServerMsgID = "serverMsgId"
	FCKUserID      = "userId"
	FCKSessionID   = "session_id"
	FCKAgentID     = "agent_id"
	FCKQuery       = "query"
	FCKGdpCtx      = "gdpCtx"

	// Phase 4: fast-path parameters
	FCKRequestType = "requestType"
	FCKResume      = "resume"
	// FCKOriginMsgID is the previous-step message ID echoed by the frontend after card confirmation (for context assembly).
	// The frontend takes originMsgId from the submit_clarification response and sends it back on the next request;
	// fastpath uses it to trace the preceding script (aligned with the reference system's OriginMsgID chain).
	FCKOriginMsgID = "origin_msg_id"
)

// ConversationRequest is the HTTP JSON request body parsed by con_reader.
type ConversationRequest struct {
	UserID    int64  `json:"userId"`
	SessionID string `json:"sessionId"`
	AgentID   string `json:"agentId"`
	Query     string `json:"query"`
	// Reconnect holds reconnect-replay parameters (sent by the frontend after an EventSource disconnect)
	Reconnect bool   `json:"reconnect"`
	MsgID     string `json:"msgId"`
	LastSeq   int64  `json:"lastSeq"`

	// Phase 4: fast-path parameters
	RequestType string `json:"requestType"` // "" / "retry" / "cron"
	Resume      bool   `json:"resume"`      // HIL breakpoint resume
	// OriginMsgID is the previous-step message ID echoed by the frontend after card confirmation (fastpath uses it to trace the preceding script).
	OriginMsgID string `json:"originMsgId"`
}

// ConversationLoadResult holds the data produced by con_reader during LoadInput.
type ConversationLoadResult struct {
	Request ConversationRequest
}

// ConReaderNode is the entry operator of the conversation flow.
//
// Responsibilities: parse the HTTP request → find/create session and message_record → (async branching) → write the flow context.
//
// Phase 2: when asyncEnabled=true, tryEnqueueAsync sends the request to a Redis Stream
// for background Workers to execute asynchronously; any step failing silently falls back to the sync path.
type ConReaderNode struct {
	sessionRepo       repository.SessionRepository       // cg_session DAO (find/create sessions)
	messageRecordRepo repository.MessageRecordRepository // cg_message_record DAO (create message records)
	asyncRepo         repository.AsyncRequestRepository  // cg_async_request DAO (persist async requests; async disabled when nil)
	streamQ           infra.StreamQueue                  // message queue (XADD dispatches async tasks; async disabled when nil)
	locker            infra.Locker                       // idempotency lock (SETNX; async disabled when nil)
	asyncEnabled      bool                               // async switch, controlled by the ASYNC_ENABLED env var

	In ConversationLoadResult
}

// NewConReaderNode creates a ConReaderNode.
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

// Name returns the operator name (referenced in the flow config).
func (op *ConReaderNode) Name() string { return "con_reader" }

// DoImpl implements the four-stage operator skeleton: LoadInput → Call → (tryEnqueueAsync) → WriteOutput.
func (op *ConReaderNode) DoImpl(ctx *seda.Context) (int, string) {
	httpBody, ok := ctx.Get(FCKGdpCtx).([]byte)
	if !ok {
		return -1, "con_reader: no HTTP body in the context"
	}
	if err := op.loadInput(httpBody); err != nil {
		return -1, fmt.Sprintf("con_reader: parse error: %v", err)
	}

	// Restore the HTTP span from the flow context and wrap con_reader.* spans.
	// The subsequent session/message_record SQL, Redis SetNX, and XADD traceparent all hang under the HTTP trace.
	gctx := tracex.ContextFromFlow(ctx)
	gctx, span := otel.Tracer("github.com/hangtiancheng/yukino.go/apps/creative_agent/server/operators").Start(gctx, "con_reader.process")
	defer span.End()
	span.SetAttributes(attribute.String("session_id", op.In.Request.SessionID))

	if err := op.call(gctx, ctx); err != nil {
		return -1, fmt.Sprintf("con_reader: %v", err)
	}

	// Phase 2.5: reconnect → reuse the existing record → force the attach replay
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
		return fmt.Errorf("invalid JSON: %w", err)
	}
	// Reconnect replays carry no query (the frontend openReplay sends only msgId+lastSeq); skip the required-field check.
	if req.Query == "" && !(req.Reconnect && req.MsgID != "") {
		return fmt.Errorf("query is required")
	}
	if req.SessionID == "" {
		return fmt.Errorf("sessionId is required")
	}
	if req.AgentID == "" {
		return fmt.Errorf("agentId is required")
	}
	if req.UserID == 0 {
		return fmt.Errorf("userId is required")
	}
	op.In.Request = req
	return nil
}

func (op *ConReaderNode) call(gctx context.Context, ctx *seda.Context) error {
	req := op.In.Request

	// 1. Reconnect: reuse the existing record specified by the frontend; skip record creation
	if req.Reconnect && req.MsgID != "" {
		existing, err := op.messageRecordRepo.GetByMessageID(gctx, req.MsgID)
		if err == nil && existing != nil {
			ctx.Set("_messageID", existing.MessageID)
			ctx.Set("_reconnectMsgID", existing.MessageID)
			ctx.Set("_lastSeq", req.LastSeq)
			return nil
		}
		// Record missing → fall back to sync creation (a normal request)
	}

	// 2. Determine message_id: create a new one per request (snowflake ID).
	//    Do not reuse the "latest InProgress" record — that would cross-wire two consecutive messages
	//    in the same session onto one msgID (account mixing). Resume is located by the frontend explicitly sending msgId (see reconnect).
	msgID := model.NextIDString()

	// 3. Find or create the session
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
			return fmt.Errorf("create session: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("query session: %w", err)
	}

	// 3. Create message_record
	ct := model.NewChatText(req.Query)
	ctJSON, err := json.Marshal(ct)
	if err != nil {
		return fmt.Errorf("marshal chat_text: %w", err)
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
	// Write trace_id into log_id so the DB can be queried back to the trace.
	if sc := trace.SpanContextFromContext(gctx); sc.IsValid() {
		rec.LogID = sc.TraceID().String()
	}
	if err := op.messageRecordRepo.Insert(gctx, rec); err != nil {
		return fmt.Errorf("create message_record: %w", err)
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

	// Phase 4: fast-path parameter pass-through
	ctx.Set(FCKRequestType, req.RequestType)
	ctx.Set(FCKResume, req.Resume)
	ctx.Set(FCKOriginMsgID, req.OriginMsgID)

	// Phase 2.5: reconnect pass-through
	if rID, ok := ctx.Get("_reconnectMsgID").(string); ok && rID != "" {
		ctx.Set("reconnectMsgId", rID)
	}
	if ls, ok := ctx.Get("_lastSeq").(int64); ok {
		ctx.Set("lastSeq", ls)
	}
}

// tryEnqueueAsync attempts to make the request async: idempotency lock → persist → push to stream.
// Any step failing silently falls back to the sync path (the request is not blocked).
// gctx carries the HTTP span restored from the flow context: SetNX/Insert/XADD all hang under the HTTP trace.
func (op *ConReaderNode) tryEnqueueAsync(gctx context.Context, ctx *seda.Context) {
	req := op.In.Request
	msgID, _ := ctx.Get("_messageID").(string)

	// 1. 5s-window idempotency: prevents rapid duplicate clicks
	dedupKey := computeDedupKey(req.SessionID, msgID, req.Query)
	if op.locker == nil {
		return // lock unavailable → fall back to sync
	}
	acquired, err := op.locker.SetNX(gctx, dedupKey, msgID, 5*time.Second)
	if err != nil || !acquired {
		return // idempotency conflict or lock unavailable → fall back to sync
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
	// Write trace_id into log_id so the DB can be queried back to the trace.
	if tid := tracex.TraceIDFromFlow(ctx); tid != "" {
		asyncReq.LogID = tid
	}
	if err := op.asyncRepo.InsertPending(gctx, asyncReq); err != nil {
		_ = op.locker.Del(gctx, dedupKey) // release the idempotency lock
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
		return // already persisted; recovery covers it
	}

	// 4. Success: mark the async short-circuit
	ctx.Set("asyncShortCircuit", true)
	ctx.Set("asyncRequestId", asyncReqID)
}

// computeDedupKey generates the 5s-window idempotency dedup key from session+msgID+query.
func computeDedupKey(sessionID, msgID, query string) string {
	raw := sessionID + "|" + msgID + "|" + query
	sum := md5.Sum([]byte(raw))
	return "cg:dedup:" + hex.EncodeToString(sum[:])
}
