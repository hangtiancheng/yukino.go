package bridge

import "strings"

func normalizedModel(model string) string {
	return strings.ReplaceAll(strings.ToLower(model), ".", "-")
}

func adaptiveThinking(model string) bool {
	model = normalizedModel(model)
	for _, prefix := range []string{
		"claude-opus-4-6", "claude-sonnet-4-6", "claude-opus-4-7", "claude-opus-4-8",
		"claude-opus-5", "claude-sonnet-5", "claude-fable-5", "claude-mythos-5", "claude-mythos-preview",
	} {
		if strings.Contains(model, prefix) {
			return true
		}
	}
	return false
}

func requiresThinking(model string) bool {
	model = normalizedModel(model)
	for _, prefix := range []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5", "claude-mythos-5", "claude-mythos-preview"} {
		if strings.Contains(model, prefix) {
			return true
		}
	}
	return false
}

func anthropicEffort(model, effort string) string {
	if effort == "none" || effort == "minimal" {
		return "low"
	}
	if effort == "xhigh" && strings.Contains(normalizedModel(model), "4-6") {
		return "max"
	}
	return effort
}
