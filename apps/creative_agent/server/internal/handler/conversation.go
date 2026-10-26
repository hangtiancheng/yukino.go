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

// NewConversationHandler 返回 /aigc/conversation 的 HTTP handler。
// 同时支持 GET（供 EventSource 使用）和 POST（供 XHR/fetch 使用）。
// 只做 HTTP 参数绑定 → 调 business 入口 NewFlow → WaitExec。
func NewConversationHandler(deps conversation.FlowDeps) yukino.Middleware {
	return func(c *yukino.Context, _ func()) {
		var body []byte

		if c.Request.Method == http.MethodGet {
			// EventSource GET：从 query string 拼出请求体。
			// userId 可信来源只有 JWT 中间件注入的 ctx（会话绑定个人），
			// 缺失时保持 0，由 con_reader 的必填校验拒绝，不接受 query 伪造。
			q := c.Query("query")
			sid := c.Query("sessionId")
			aid := c.Query("agentId")

			// 从 JWT 中间件取可信 user_id（会话绑定个人）。
			var uid int64
			if v, ok := c.State[middleware.ContextUserID].(int64); ok {
				uid = v
			}

			// Phase 2.5: 断线重连回放参数
			reconnect := c.Query("reconnect") == "1"
			msgID := c.Query("msgId")
			var lastSeq int64
			if ls := c.Query("lastSeq"); ls != "" {
				lastSeq, _ = strconv.ParseInt(ls, 10, 64)
			}

			// Phase 4: 快捷路径参数
			requestType := c.Query("requestType")
			resume := c.Query("resume") == "1"
			// 点卡确认后前端回传的 originMsgId（fastpath 追溯前序脚本用）
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
			// POST 的 body userId 同样不可信，用 JWT 中间件注入的 user_id 覆盖（会话绑定个人）。
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

		// SSE 流式响应：ctx.SSE() 写响应头 + 200 并接管响应（NewWriter 内部调用）。
		c.Set("X-Accel-Buffering", "no")
		sseWriter := sse.NewWriter(c)

		conf := conversation.NewFlow(sseWriter, deps)
		flow := seda.BuildFlow(conf)
		// 把 HTTP ctx 的 trace 注入 flow context，供 con_reader/supervisor_agent 恢复。
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
