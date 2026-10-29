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

// Build a temporary skills directory + weather/SKILL.md
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
description: Query city weather. Call when the user asks about weather, temperature, or rain.
apis:
  - name: query_weather
    description: Query the current weather of the given city
    endpoint: ${WEATHER_API_BASE_URL}/api/weather
    method: GET
    parameters:
      city:
        type: string
        description: City name (in Chinese, e.g. 北京)
        required: true
      user_id:
        type: string
        required: true
        injected: true
---
# weather skill usage
To query the weather, call the query_weather tool with city.
`
	testx.NoError(t, os.WriteFile(filepath.Join(weatherDir, "SKILL.md"), []byte(skill), 0o644))
	t.Setenv("WEATHER_API_BASE_URL", srv.URL)
	return root, gotQuery, gotBody, closeFn
}

// FSRepository scan → Summaries contains weather
func TestFSRepository_Summaries(t *testing.T) {
	root, _, _, closeFn := writeWeatherSkill(t)
	defer closeFn()
	repo, err := appkitskill.NewFSRepository(root)
	testx.NoError(t, err)
	testx.Len(t, repo.Summaries(), 1)
	testx.Equal(t, "weather", repo.Summaries()[0].Name)
	testx.Equal(t, "Query city weather. Call when the user asks about weather, temperature, or rain.", repo.Summaries()[0].Description)
}

// query_weather is invisible before skill_load and visible after
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
	testx.NotContains(t, names, "query_weather", "APIs of unloaded skills are invisible")

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
	testx.Contains(t, names, "query_weather", "the API should be exposed after loading")
}

// Generic executor: GET → query string + injected parameter injection + ${VAR} expansion
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
	testx.Contains(t, gotQuery, "user_id", "injected parameters should be provided server-side")
	_ = gotBody
}

// Unknown tool → handled=false
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
