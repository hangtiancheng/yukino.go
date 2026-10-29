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

// TestSubmitClarification_AdvanceStage verifies that card confirmation advances FlowStage.
func TestSubmitClarification_AdvanceStage(t *testing.T) {
	memory := memory.NewIntentMemory()

	r := router.Setup(router.Dependencies{
		UserSvc:      nil,
		JWTSecret:    "test-secret",
		Redis:        nil,
		Logger:       logger.New("error"),
		IntentMemory: memory,
	})

	// Task 8: conversation routes require auth; card confirmation must carry the user JWT.
	token, err := service.NewAuthService("test-secret").GenerateToken("alice", 7)
	testx.NoError(t, err)

	// Topic card confirmation
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

	// Topic card → topic_confirmed, agent not advanced
	res := do("topic", "mix_video", string(subtask.AgentIdeaKing), "")
	testx.Equal(t, "topic_confirmed", res["stage"])
	testx.Equal(t, "", res["nextAgent"])

	// Script card → script_confirmed, next agent computed (edit_master), originMsgId echoed
	res2 := do("script", "mix_video", string(subtask.AgentGoldScriptwriter), "msg-script-1")
	testx.Equal(t, "script_confirmed", res2["stage"])
	testx.Equal(t, string(subtask.AgentEditMaster), res2["nextAgent"])
	testx.Equal(t, "msg-script-1", res2["originMsgId"])

	// CanFastTask should hit after the script card
	testx.True(t, memory.GetStage("s1").CanFastTask())
}

// TestSubmitClarification_Validation verifies required-field and validity checks.
func TestSubmitClarification_Validation(t *testing.T) {
	r := router.Setup(router.Dependencies{
		UserSvc:      nil,
		JWTSecret:    "test-secret",
		Redis:        nil,
		Logger:       logger.New("error"),
		IntentMemory: memory.NewIntentMemory(),
	})

	// Task 8: conversation routes require auth; card confirmation must carry the user JWT.
	token, err := service.NewAuthService("test-secret").GenerateToken("alice", 7)
	testx.NoError(t, err)
	auth := func(req *http.Request) *http.Request {
		req.Header.Set("Authorization", "Bearer "+token)
		return req
	}

	// Missing sessionId → 400
	body, _ := json.Marshal(map[string]string{"cardType": "script"})
	req := auth(httptest.NewRequest(http.MethodPost, "/aigc/agent/submit_clarification", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	testx.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid cardType → 400
	body2, _ := json.Marshal(map[string]string{"sessionId": "s1", "cardType": "bogus"})
	req2 := auth(httptest.NewRequest(http.MethodPost, "/aigc/agent/submit_clarification", bytes.NewReader(body2)))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	testx.Equal(t, http.StatusBadRequest, w2.Code)

	// Script card missing msgId → 400 (cannot trace the preceding script)
	body3, _ := json.Marshal(map[string]string{
		"sessionId": "s1", "cardType": "script", "agentId": "gold_scriptwriter", "pipeline": "mix_video",
	})
	req3 := auth(httptest.NewRequest(http.MethodPost, "/aigc/agent/submit_clarification", bytes.NewReader(body3)))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	testx.Equal(t, http.StatusBadRequest, w3.Code)

	// Script card missing pipeline → 200, but nextAgent is empty (cannot compute the next one); msgId echoed
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

// TestSubmitClarification_HotTopic_SelectedEid verifies hot-topic card confirmation:
// carrying selectedEid/topicTitle → written into IntentMemory (GetTopicEid/GetTopicTitle) +
// the response echoes selectedEid/originMsgId.
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
	testx.Equal(t, "eid-abc123", resp.Data.SelectedEid, "the response should echo selectedEid")
	testx.Equal(t, "选题一", resp.Data.TopicTitle)
	testx.Equal(t, "msg-hot-1", resp.Data.OriginMsgID)

	// IntentMemory holds the clicked hot-topic eid + topic title
	testx.Equal(t, "eid-abc123", im.GetTopicEid("s-hot"))
	testx.Equal(t, "选题一", im.GetTopicTitle("s-hot"))
}

// Ensure the handler package's core functions are referenced (compile check).
var _ = handler.ClarificationRequest{}
