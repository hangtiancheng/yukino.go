package ideaking

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
)

// MockProvider 是未配置真实热点服务时的回退实现：返回带 eid 的结构化选题。
// eid 用 md5(query+"|"+i) 生成，保证同一 query 下稳定、可被 submit_clarification 回传。
type MockProvider struct{}

// NewMockProvider 创建 MockProvider。
func NewMockProvider() *MockProvider { return &MockProvider{} }

// SearchHotTopics 返回 3 条带 eid 的选题 + 热点 markdown 摘要。
func (m *MockProvider) SearchHotTopics(_ context.Context, query string, count int) (*HotTopicResult, error) {
	if query == "" {
		query = "热点创作"
	}
	if count <= 0 {
		count = 3
	}
	if count > 5 {
		count = 5
	}

	topics := []struct {
		title string
		desc  string
	}{
		{"深度解读：2026 上半年行业趋势与机会", "从数据看趋势，为你拆解赛道机会与挑战。"},
		{"热点观察：「全民 AI 创作」背后的内容生态变革", "工具普及如何改变创作门槛？新玩法全解析。"},
		{"情绪共鸣：都市年轻人的「精神内耗」自救指南", "切中当下情绪热点，容易引发转发与讨论。"},
		{"冷门掘金：小众圈层的高粘性内容怎么做", "避开红海，用小众切口做出爆款。"},
		{"破圈案例：一条视频从 0 到 1000w 播放的秘密", "拆解爆款背后的选题、脚本与节奏。"},
	}

	out := &HotTopicResult{
		HotspotData: fmt.Sprintf("围绕「%s」为你整理了 5 个高潜选题，挑一个开工吧～", query),
	}
	for i := 0; i < count && i < len(topics); i++ {
		h := md5.Sum([]byte(fmt.Sprintf("%s|%d", query, i)))
		out.TitleData = append(out.TitleData, TitleItem{
			Title:   topics[i].title,
			Content: topics[i].desc,
			Eid:     hex.EncodeToString(h[:8]),
		})
	}
	return out, nil
}
