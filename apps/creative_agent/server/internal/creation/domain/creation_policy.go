package domain

import "strconv"

// itoa is a shortcut for int-to-string (avoiding strconv imports everywhere).
func itoa(v int) string { return strconv.Itoa(v) }

// TaskTypeKey generates the config key of a (role, type) combination, e.g. "5:mix_video".
func TaskTypeKey(roleType int, projectType string) string {
	if projectType == "" {
		return itoa(roleType) + ":-"
	}
	return itoa(roleType) + ":" + projectType
}

// CalculateCost bills differentially by creation type/parameters.
//
// The original project's CalculatePointCost differentiates unit prices by type; the reproduction keeps type-level differentiation,
// refinable later by parameters in project_input (duration/count, etc.).
func CalculateCost(projectType string, projectInput map[string]any) int {
	base := map[string]int{
		ProjectTypeMixVideo:      10,
		ProjectTypeMiaoyingVideo: 8,
		ProjectTypeMiaotuImage:   5,
		ProjectTypeGraphicNotes:  3,
	}
	cost, ok := base[projectType]
	if !ok {
		return 10 // fallback: unknown types are priced as mix-cut
	}
	// Secondary surcharge by parameter: +2 points per 30s of duration (mix-cut / instant video)
	if secs, ok := projectInput["duration_seconds"].(float64); ok && secs > 30 {
		cost += int(secs/30) * 2
	}
	return cost
}
