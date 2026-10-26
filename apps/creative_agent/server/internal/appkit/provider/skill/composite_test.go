package appkitskill_test

import (
	"context"
	"testing"

	appkitskill "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/skill"
	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

func TestCompositeProvider_MergeAndExecute(t *testing.T) {
	root, _, _, closeFn := writeWeatherSkill(t)
	defer closeFn()
	repo, err := appkitskill.NewFSRepository(root)
	testx.NoError(t, err)
	skillSP := appkitskill.NewSkillProvider(repo, appkitskill.NewRemoteSkillExecutor(nil))
	builtin := appkittools.NewSubAgentToolRegistry()

	cp := appkittools.NewCompositeProvider(builtin, skillSP)

	defs, err := cp.GetFunctionDefs(context.Background())
	testx.NoError(t, err)
	var names []string
	for _, d := range defs {
		names = append(names, d.Name)
	}
	// 内建工具 + skill_load（query_weather 未 load 不可见）
	testx.Contains(t, names, "search")
	testx.Contains(t, names, "skill_load")
	testx.NotContains(t, names, "query_weather")

	// 内建工具执行（skill 同名时不冲突）
	res, handled, _, err := cp.Execute(context.Background(), "search", `{"query":"golang"}`)
	testx.NoError(t, err)
	testx.True(t, handled)
	testx.Contains(t, res, "golang")

	// 未知工具 → handled=false
	_, handled, _, _ = cp.Execute(context.Background(), "nope", "{}")
	testx.False(t, handled)
}
