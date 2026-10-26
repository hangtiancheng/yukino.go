package domain

// projectTypeConsumeTitle 各作品类型扣积分标题。
var projectTypeConsumeTitle = map[string]string{
	"mix_video":     "度加AI-AI成片",
	"miaoying":      "度加AI-视频生成",
	"miaotu":        "度加AI-图片生成",
	"graphic_notes": "度加AI-AI笔记图",
}

// projectTypeBackTitle 各作品类型退积分标题。
var projectTypeBackTitle = map[string]string{
	"mix_video":     "度加AI-AI成片失败退回",
	"miaoying":      "度加AI-视频生成失败退回",
	"miaotu":        "度加AI-图片生成失败退回",
	"graphic_notes": "度加AI-AI笔记图失败退回",
}

// ConsumeTitle 查扣积分标题，兜底"度加AI-AI成片"。
func ConsumeTitle(projectType string) string {
	if t, ok := projectTypeConsumeTitle[projectType]; ok {
		return t
	}
	return "度加AI-AI成片"
}

// BackTitle 查退还积分标题，兜底"度加AI-AI成片失败退回"。
func BackTitle(projectType string) string {
	if t, ok := projectTypeBackTitle[projectType]; ok {
		return t
	}
	return "度加AI-AI成片失败退回"
}
