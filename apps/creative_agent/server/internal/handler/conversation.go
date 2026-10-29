package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	conversation "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/middleware"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/sse"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/tracex"

	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
)

// NewConversationHandler returns the HTTP handler for /aigc/conversation.
// It supports both GET (for EventSource) and POST (for XHR/fetch).
// It only binds HTTP parameters → calls the business entry NewFlow → WaitExec.
func NewConversationHandler(deps conversation.FlowDeps) yukino.Middleware {
	return func(c *yukino.Context, _ func()) {
		var body []byte

		if c.Request.Method == http.MethodGet {
			// EventSource GET: assemble the request body from the query string.
			// The only trusted source of userId is the ctx injected by the JWT middleware (sessions are bound to individuals);
			// when missing it stays 0 and is rejected by con_reader's required-field validation — query spoofing is not accepted.
			q := c.Query("query")
			sid := c.Query("sessionId")
			aid := c.Query("agentId")

			// Take the trusted user_id from the JWT middleware (sessions are bound to individuals).
			var uid int64
			if v, ok := c.State[middleware.ContextUserID].(int64); ok {
				uid = v
			}

			// Phase 2.5: reconnect replay parameters
			reconnect := c.Query("reconnect") == "1"
			msgID := c.Query("msgId")
			var lastSeq int64
			if ls := c.Query("lastSeq"); ls != "" {
				lastSeq, _ = strconv.ParseInt(ls, 10, 64)
			}

			// Phase 4: fast-path parameters
			requestType := c.Query("requestType")
			resume := c.Query("resume") == "1"
			// originMsgId sent back by the frontend after a card confirmation (fastpath uses it to trace the preceding script)
			originMsgID := c.Query("originMsgId")

			body, _ = json.Marshal(map[string]interface{}{
				"query": q, "sessionId": sid, "agentId": aid, "userId": uid,
				"reconnect": reconnect, "msgId": msgID, "lastSeq": lastSeq,
				"requestType": requestType, "resume": resume, "originMsgId": originMsgID,
			})
		} else {
			var err error
			body, err = io.ReadAll(c.Request.Body)
			if err != nil {
				c.SetStatus(http.StatusBadRequest)
				c.JSON(yukino.H{"error": "cannot read body"})
				return
			}
			// The POST body userId is equally untrusted; override it with the user_id injected by the JWT middleware (sessions are bound to individuals).
			if v, ok := c.State[middleware.ContextUserID]; ok {
				var m map[string]interface{}
				if json.Unmarshal(body, &m) == nil {
					if m == nil {
						m = map[string]interface{}{}
					}
					m["userId"] = v.(int64)
					body, _ = json.Marshal(m)
				}
			}
		}

		// SSE streaming response: ctx.SSE() writes the response headers + 200 and takes over the response (called inside NewWriter).
		c.Set("X-Accel-Buffering", "no")
		sseWriter := sse.NewWriter(c)

		conf := conversation.NewFlow(sseWriter, deps)
		flow := seda.BuildFlow(conf)
		// Inject the HTTP ctx trace into the flow context so con_reader/supervisor_agent can restore it.
		tracex.InjectToFlow(flow.GetContext(), c.Request.Context())
		flow.GetContext().Set(conversation.FCKGdpCtx, body)

		code, msg := flow.WaitExec()
		if code < 0 {
			_ = sseWriter.WriteEvent(sse.EventError, sse.Payload{
				Type:    sse.EventError,
				Content: msg,
			})
		}
	}
}
