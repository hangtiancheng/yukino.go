package appkitskill_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	appkitskill "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/skill"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// 构造临时 skills 目录 + weather/SKILL.md
func writeWeatherSkill(t *testing.T) (root string, gotQuery map[string]string, gotBody map[string]any, closeFn func()) {
	t.Helper()
	root = t.TempDir()
	weatherDir := filepath.Join(root, "weather")
	testx.NoError(t, os.MkdirAll(weatherDir, 0o755))

	gotQuery = map[string]string{}
	gotBody = map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		testx.Equal(t, "/api/weather", r.URL.Path)
		for k, v := range r.URL.Query() {
			gotQuery[k] = v[0]
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"city":"北京","temperature":28,"unit":"celsius"}`))
	}))
	closeFn = srv.Close

	skill := `---
name: weather
description: 查询城市天气。当用户询问天气、气温、降雨时调用。
apis:
  - name: query_weather
    description: 查询指定城市的当前天气
    endpoint: ${WEATHER_API_BASE_URL}/api/weather
    method: GET
    parameters:
      city:
        type: string
        description: 城市名（中文）
        required: true
      user_id:
        type: string
        required: true
        injected: true
---
# weather 使用说明
查询天气时调用 query_weather 工具，传入 city。
`
	testx.NoError(t, os.WriteFile(filepath.Join(weatherDir, "SKILL.md"), []byte(skill), 0o644))
	t.Setenv("WEATHER_API_BASE_URL", srv.URL)
	return root, gotQuery, gotBody, closeFn
}

// FSRepository 扫描 → Summaries 含 weather
func TestFSRepository_Summaries(t *testing.T) {
	root, _, _, closeFn := writeWeatherSkill(t)
	defer closeFn()
	repo, err := appkitskill.NewFSRepository(root)
	testx.NoError(t, err)
	testx.Len(t, repo.Summaries(), 1)
	testx.Equal(t, "weather", repo.Summaries()[0].Name)
	testx.Equal(t, "查询城市天气。当用户询问天气、气温、降雨时调用。", repo.Summaries()[0].Description)
}

// skill_load 前 query_weather 不可见，skill_load 后可见
func TestSkillProvider_LazyLoad(t *testing.T) {
	root, _, _, closeFn := writeWeatherSkill(t)
	defer closeFn()
	repo, err := appkitskill.NewFSRepository(root)
	testx.NoError(t, err)
	sp := appkitskill.NewSkillProvider(repo, appkitskill.NewRemoteSkillExecutor(nil))

	defs, err := sp.GetFunctionDefs(context.Background())
	testx.NoError(t, err)
	var names []string
	for _, d := range defs {
		names = append(names, d.Name)
	}
	testx.Contains(t, names, "skill_load")
	testx.NotContains(t, names, "query_weather", "未 load 的 skill API 不可见")

	// skill_load
	res, handled, _, err := sp.Execute(context.Background(), "skill_load", `{"skill":"weather"}`)
	testx.NoError(t, err)
	testx.True(t, handled)
	testx.Contains(t, res, "weather")

	defs, err = sp.GetFunctionDefs(context.Background())
	testx.NoError(t, err)
	names = names[:0]
	for _, d := range defs {
		names = append(names, d.Name)
	}
	testx.Contains(t, names, "query_weather", "load 后 API 应暴露")
}

// 通用执行器：GET → query string + injected 参数注入 + ${VAR} 展开
func TestSkillProvider_ExecuteWeather(t *testing.T) {
	root, gotQuery, gotBody, closeFn := writeWeatherSkill(t)
	defer closeFn()
	repo, err := appkitskill.NewFSRepository(root)
	testx.NoError(t, err)
	sp := appkitskill.NewSkillProvider(repo, appkitskill.NewRemoteSkillExecutor(nil))
	_, _, _, err = sp.Execute(context.Background(), "skill_load", `{"skill":"weather"}`)
	testx.NoError(t, err)

	res, handled, isPipeline, err := sp.Execute(context.Background(), "query_weather", `{"city":"北京"}`)
	testx.NoError(t, err)
	testx.True(t, handled)
	testx.False(t, isPipeline)
	testx.Contains(t, res, `"temperature":28`)
	testx.Equal(t, "北京", gotQuery["city"])
	testx.Contains(t, gotQuery, "user_id", "injected 参数应由服务端注入")
	_ = gotBody
}

// 未知工具 → handled=false
func TestSkillProvider_UnknownTool(t *testing.T) {
	root, _, _, closeFn := writeWeatherSkill(t)
	defer closeFn()
	repo, err := appkitskill.NewFSRepository(root)
	testx.NoError(t, err)
	sp := appkitskill.NewSkillProvider(repo, appkitskill.NewRemoteSkillExecutor(nil))
	_, handled, _, err := sp.Execute(context.Background(), "nope", "{}")
	testx.Error(t, err)
	testx.False(t, handled)
}
