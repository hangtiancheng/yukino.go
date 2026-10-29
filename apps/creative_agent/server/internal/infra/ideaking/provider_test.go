package ideaking_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/infra/ideaking"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// TestHTTPProvider_SearchHotTopics verifies that the HTTP implementation parses the external service response (title_data/hotspot_data).
func TestHTTPProvider_SearchHotTopics(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		testx.Equal(t, "/inner/ideaking/hot_topics", r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"hotspot_data":"## 热点","title_data":[{"title":"选题一","content":"描述一","eid":"abc123"}]}}`))
	}))
	defer srv.Close()

	p := ideaking.NewHTTPProvider(ideaking.HTTPConfig{BaseURL: srv.URL})
	res, err := p.SearchHotTopics(context.Background(), "城市夜景", 3)
	testx.NoError(t, err)
	testx.Len(t, res.TitleData, 1)
	testx.Equal(t, "选题一", res.TitleData[0].Title)
	testx.Equal(t, "描述一", res.TitleData[0].Content)
	testx.Equal(t, "abc123", res.TitleData[0].Eid)
	testx.Contains(t, res.HotspotData, "热点")

	testx.Equal(t, "城市夜景", gotBody["query"])
	testx.Equal(t, float64(3), gotBody["hotspot_count"])
}

// TestHTTPProvider_Errno verifies that a non-zero business errno returns an error.
func TestHTTPProvider_Errno(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":5001,"message":"service error","data":null}`))
	}))
	defer srv.Close()

	p := ideaking.NewHTTPProvider(ideaking.HTTPConfig{BaseURL: srv.URL})
	_, err := p.SearchHotTopics(context.Background(), "q", 3)
	testx.Error(t, err)
	testx.Contains(t, err.Error(), "5001")
}

// TestHTTPProvider_HTTPError verifies that a non-200 response returns an error.
func TestHTTPProvider_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := ideaking.NewHTTPProvider(ideaking.HTTPConfig{BaseURL: srv.URL})
	_, err := p.SearchHotTopics(context.Background(), "q", 3)
	testx.Error(t, err)
	testx.Contains(t, err.Error(), "500")
}

// TestMockProvider_Structure verifies that the mock returns structured topics with eids (usable for the demo loop).
func TestMockProvider_Structure(t *testing.T) {
	p := ideaking.NewMockProvider()
	res, err := p.SearchHotTopics(context.Background(), "热点", 3)
	testx.NoError(t, err)
	testx.Len(t, res.TitleData, 3)
	for _, ti := range res.TitleData {
		testx.NotEmpty(t, ti.Title, "the topic title should be non-empty")
		testx.NotEmpty(t, ti.Content, "the topic description should be non-empty")
		testx.NotEmpty(t, ti.Eid, "the topic eid should be non-empty (echoed on card click to locate the hotspot)")
	}
	testx.NotEmpty(t, res.HotspotData)
}

// TestBaiduHotProvider_SearchHotTopics verifies Baidu hot-search parsing (simulating the top.baidu.com response).
func TestBaiduHotProvider_SearchHotTopics(t *testing.T) {
	// Simulate the Baidu hot-search response structure
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"cards":[{"content":[{"content":[{"isTop":false,"index":1,"word":"胖东来员工回应工资及闭店后安置","hotTag":"3"},{"isTop":false,"index":2,"word":"BBA集体大降价：宝马5系跌至28万","hotTag":"1"},{"isTop":false,"index":3,"word":"暴雨天留心4个风险点","hotTag":"0"}]}]}]}}`))
	}))
	defer srv.Close()

	p := ideaking.NewBaiduHotProvider()
	// Override the built-in URL to point at the test server
	origURL := ideaking.BaiduHotURL()
	ideaking.SetBaiduHotURL(srv.URL)
	defer ideaking.SetBaiduHotURL(origURL)

	res, err := p.SearchHotTopics(context.Background(), "宝马", 5)
	testx.NoError(t, err)
	testx.NotEmpty(t, res.TitleData)
	// query=宝马 should match "BBA集体大降价：宝马5系跌至28万"
	testx.Equal(t, "BBA集体大降价：宝马5系跌至28万", res.TitleData[0].Title)
	testx.NotEmpty(t, res.TitleData[0].Eid, "topics should carry an eid")
	testx.NotEmpty(t, res.HotspotData)
}

// TestBaiduHotProvider_NetworkError_Fallback verifies that FallbackProvider degrades when Baidu hot search fails.
func TestBaiduHotProvider_NetworkError_Fallback(t *testing.T) {
	// Point at an unreachable address → primary provider fails → fall back to the mock
	origURL := ideaking.BaiduHotURL()
	ideaking.SetBaiduHotURL("http://127.0.0.1:1")
	defer ideaking.SetBaiduHotURL(origURL)

	p := ideaking.NewFallbackProvider(ideaking.NewBaiduHotProvider(), ideaking.NewMockProvider())
	res, err := p.SearchHotTopics(context.Background(), "热点", 3)
	testx.NoError(t, err, "a Baidu hot-search failure should degrade to the mock without error")
	testx.NotEmpty(t, res.TitleData, "the mock returns topics with eids")
	testx.NotEmpty(t, res.TitleData[0].Eid)
}
