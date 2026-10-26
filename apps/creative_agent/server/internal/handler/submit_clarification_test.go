package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/conversation/memory"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/business/subtask"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/handler"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/router"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/service"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/logger"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestSubmitClarification_AdvanceStage 验证点卡确认推进 FlowStage。
func TestSubmitClarification_AdvanceStage(t *testing.T) {
	memory := memory.NewIntentMemory()

	r := router.Setup(router.Dependencies{
		UserSvc:      nil,
		JWTSecret:    "test-secret",
		Redis:        nil,
		Logger:       logger.New("error"),
		IntentMemory: memory,
	})

	// Task 8: 对话路由强制鉴权，点卡确认需携带用户 JWT。
	token, err := service.NewAuthService("test-secret").GenerateToken("alice", 7)
	testx.NoError(t, err)

	// 选题卡确认
	do := func(cardType, pipeline, agentID, msgID string) map[string]any {
		reqBody := map[string]string{
			"sessionId": "s1",
			"cardType":  cardType,
			"pipeline":  pipeline,
			"agentId":   agentID,
		}
		if msgID != "" {
			reqBody["msgId"] = msgID
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost,
			"/aigc/agent/submit_clarification", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		testx.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Code int `json:"code"`
			Data struct {
				Stage       string `json:"stage"`
				Pipeline    string `json:"pipeline"`
				Agent       string `json:"agent"`
				NextAgent   string `json:"nextAgent"`
				OriginMsgID string `json:"originMsgId"`
			} `json:"data"`
		}
		testx.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return map[string]any{
			"stage": resp.Data.Stage, "nextAgent": resp.Data.NextAgent,
			"originMsgId": resp.Data.OriginMsgID,
		}
	}

	// 选题卡 → topic_confirmed，不推进 agent
	res := do("topic", "mix_video", string(subtask.AgentIdeaKing), "")
	testx.Equal(t, "topic_confirmed", res["stage"])
	testx.Equal(t, "", res["nextAgent"])

	// 脚本卡 → script_confirmed，算出下一个 agent（edit_master），回传 originMsgId
	res2 := do("script", "mix_video", string(subtask.AgentGoldScriptwriter), "msg-script-1")
	testx.Equal(t, "script_confirmed", res2["stage"])
	testx.Equal(t, string(subtask.AgentEditMaster), res2["nextAgent"])
	testx.Equal(t, "msg-script-1", res2["originMsgId"])

	// 脚本卡后 CanFastTask 应命中
	testx.True(t, memory.GetStage("s1").CanFastTask())
}

// TestSubmitClarification_Validation 验证必填与合法性校验。
func TestSubmitClarification_Validation(t *testing.T) {
	r := router.Setup(router.Dependencies{
		UserSvc:      nil,
		JWTSecret:    "test-secret",
		Redis:        nil,
		Logger:       logger.New("error"),
		IntentMemory: memory.NewIntentMemory(),
	})

	// Task 8: 对话路由强制鉴权，点卡确认需携带用户 JWT。
	token, err := service.NewAuthService("test-secret").GenerateToken("alice", 7)
	testx.NoError(t, err)
	auth := func(req *http.Request) *http.Request {
		req.Header.Set("Authorization", "Bearer "+token)
		return req
	}

	// 缺 sessionId → 400
	body, _ := json.Marshal(map[string]string{"cardType": "script"})
	req := auth(httptest.NewRequest(http.MethodPost, "/aigc/agent/submit_clarification", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	testx.Equal(t, http.StatusBadRequest, w.Code)

	// 非法 cardType → 400
	body2, _ := json.Marshal(map[string]string{"sessionId": "s1", "cardType": "bogus"})
	req2 := auth(httptest.NewRequest(http.MethodPost, "/aigc/agent/submit_clarification", bytes.NewReader(body2)))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	testx.Equal(t, http.StatusBadRequest, w2.Code)

	// 脚本卡缺 msgId → 400（无法追溯前序脚本）
	body3, _ := json.Marshal(map[string]string{
		"sessionId": "s1", "cardType": "script", "agentId": "gold_scriptwriter", "pipeline": "mix_video",
	})
	req3 := auth(httptest.NewRequest(http.MethodPost, "/aigc/agent/submit_clarification", bytes.NewReader(body3)))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	testx.Equal(t, http.StatusBadRequest, w3.Code)

	// 脚本卡缺 pipeline → 200，但 nextAgent 为空（无法算下一个）；msgId 回传
	body4, _ := json.Marshal(map[string]string{
		"sessionId": "s1", "cardType": "script", "agentId": "gold_scriptwriter", "msgId": "msg-script-2",
	})
	req4 := auth(httptest.NewRequest(http.MethodPost, "/aigc/agent/submit_clarification", bytes.NewReader(body4)))
	req4.Header.Set("Content-Type", "application/json")
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	testx.Equal(t, http.StatusOK, w4.Code)

	var resp struct {
		Data struct {
			Stage       string `json:"stage"`
			NextAgent   string `json:"nextAgent"`
			OriginMsgID string `json:"originMsgId"`
		} `json:"data"`
	}
	testx.NoError(t, json.Unmarshal(w4.Body.Bytes(), &resp))
	testx.Equal(t, "script_confirmed", resp.Data.Stage)
	testx.Empty(t, resp.Data.NextAgent)
	testx.Equal(t, "msg-script-2", resp.Data.OriginMsgID)
}

// TestSubmitClarification_HotTopic_SelectedEid 验证热点卡（topic）确认：
// 携带 selectedEid/topicTitle → 写入 IntentMemory（GetTopicEid/GetTopicTitle）+
// 响应回传 selectedEid/originMsgId。
func TestSubmitClarification_HotTopic_SelectedEid(t *testing.T) {
	im := memory.NewIntentMemory()

	r := router.Setup(router.Dependencies{
		UserSvc:      nil,
		JWTSecret:    "test-secret",
		Redis:        nil,
		Logger:       logger.New("error"),
		IntentMemory: im,
	})

	token, err := service.NewAuthService("test-secret").GenerateToken("alice", 7)
	testx.NoError(t, err)

	body, _ := json.Marshal(map[string]string{
		"sessionId":   "s-hot",
		"cardType":    "topic",
		"value":       "选题一",
		"selectedEid": "eid-abc123",
		"topicTitle":  "选题一",
		"agentId":     "idea_king",
		"msgId":       "msg-hot-1",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/aigc/agent/submit_clarification", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	testx.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Stage       string `json:"stage"`
			OriginMsgID string `json:"originMsgId"`
			SelectedEid string `json:"selectedEid"`
			TopicTitle  string `json:"topicTitle"`
		} `json:"data"`
	}
	testx.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	testx.Equal(t, "topic_confirmed", resp.Data.Stage)
	testx.Equal(t, "eid-abc123", resp.Data.SelectedEid, "响应应回传 selectedEid")
	testx.Equal(t, "选题一", resp.Data.TopicTitle)
	testx.Equal(t, "msg-hot-1", resp.Data.OriginMsgID)

	// IntentMemory 已写入点选的热点 eid + 选题标题
	testx.Equal(t, "eid-abc123", im.GetTopicEid("s-hot"))
	testx.Equal(t, "选题一", im.GetTopicTitle("s-hot"))
}

// 确保 handler 包的核心函数被引用（编译检查）。
var _ = handler.ClarificationRequest{}
