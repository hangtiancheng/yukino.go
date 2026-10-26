package domain

import "strconv"

// itoa 是 int 转字符串的快捷方式（避免每处都引 strconv）。
func itoa(v int) string { return strconv.Itoa(v) }

// TaskTypeKey 生成 (role, type) 组合的配置 key，形如 "5:mix_video"。
func TaskTypeKey(roleType int, projectType string) string {
	if projectType == "" {
		return itoa(roleType) + ":-"
	}
	return itoa(roleType) + ":" + projectType
}

// CalculateCost 按创作类型/参数差异化计费。
//
// 原项目 CalculatePointCost 按类型区分单价；复现时保留类型级差异化，
// 后续可按 project_input 中的参数（时长/张数等）细化。
func CalculateCost(projectType string, projectInput map[string]any) int {
	base := map[string]int{
		ProjectTypeMixVideo:      10,
		ProjectTypeMiaoyingVideo: 8,
		ProjectTypeMiaotuImage:   5,
		ProjectTypeGraphicNotes:  3,
	}
	cost, ok := base[projectType]
	if !ok {
		return 10 // 兜底：未知类型按混剪价
	}
	// 按参数二次加成：时长（秒）每 30s +2 积分（混剪/妙影）
	if secs, ok := projectInput["duration_seconds"].(float64); ok && secs > 30 {
		cost += int(secs/30) * 2
	}
	return cost
}
