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

// TestHTTPProvider_SearchHotTopics 验证 HTTP 实现解析外部服务响应（title_data/hotspot_data）。
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

// TestHTTPProvider_Errno 验证业务 errno 非 0 返回错误。
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

// TestHTTPProvider_HTTPError 验证非 200 返回错误。
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

// TestMockProvider_Structure 验证 mock 返回带 eid 的结构化选题（demo 闭环可用）。
func TestMockProvider_Structure(t *testing.T) {
	p := ideaking.NewMockProvider()
	res, err := p.SearchHotTopics(context.Background(), "热点", 3)
	testx.NoError(t, err)
	testx.Len(t, res.TitleData, 3)
	for _, ti := range res.TitleData {
		testx.NotEmpty(t, ti.Title, "选题标题非空")
		testx.NotEmpty(t, ti.Content, "选题描述非空")
		testx.NotEmpty(t, ti.Eid, "选题 eid 非空（用户点卡回传定位热点）")
	}
	testx.NotEmpty(t, res.HotspotData)
}

// TestBaiduHotProvider_SearchHotTopics 验证百度热搜解析（模拟 top.baidu.com 响应）。
func TestBaiduHotProvider_SearchHotTopics(t *testing.T) {
	// 模拟百度热搜响应结构
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"cards":[{"content":[{"content":[{"isTop":false,"index":1,"word":"胖东来员工回应工资及闭店后安置","hotTag":"3"},{"isTop":false,"index":2,"word":"BBA集体大降价：宝马5系跌至28万","hotTag":"1"},{"isTop":false,"index":3,"word":"暴雨天留心4个风险点","hotTag":"0"}]}]}]}}`))
	}))
	defer srv.Close()

	p := ideaking.NewBaiduHotProvider()
	// 覆写内置 URL 指向测试 server
	origURL := ideaking.BaiduHotURL()
	ideaking.SetBaiduHotURL(srv.URL)
	defer ideaking.SetBaiduHotURL(origURL)

	res, err := p.SearchHotTopics(context.Background(), "宝马", 5)
	testx.NoError(t, err)
	testx.NotEmpty(t, res.TitleData)
	// query=宝马 应匹配到"BBA集体大降价：宝马5系跌至28万"
	testx.Equal(t, "BBA集体大降价：宝马5系跌至28万", res.TitleData[0].Title)
	testx.NotEmpty(t, res.TitleData[0].Eid, "选题应带 eid")
	testx.NotEmpty(t, res.HotspotData)
}

// TestBaiduHotProvider_NetworkError_Fallback 验证百度热搜失败时 FallbackProvider 降级。
func TestBaiduHotProvider_NetworkError_Fallback(t *testing.T) {
	// 指向不可达地址 → 主 provider 失败 → fallback 到 mock
	origURL := ideaking.BaiduHotURL()
	ideaking.SetBaiduHotURL("http://127.0.0.1:1")
	defer ideaking.SetBaiduHotURL(origURL)

	p := ideaking.NewFallbackProvider(ideaking.NewBaiduHotProvider(), ideaking.NewMockProvider())
	res, err := p.SearchHotTopics(context.Background(), "热点", 3)
	testx.NoError(t, err, "百度热搜失败应降级 mock，不报错")
	testx.NotEmpty(t, res.TitleData, "mock 返回带 eid 的选题")
	testx.NotEmpty(t, res.TitleData[0].Eid)
}
