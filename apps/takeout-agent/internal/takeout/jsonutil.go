package takeout

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// ExtractJSONObject extracts the first top-level JSON object from raw LLM
// output, tolerating markdown code fences and surrounding prose.
func ExtractJSONObject(content string) (map[string]any, error) {
	return extractJSON[map[string]any](content, '{', '}')
}

// ExtractJSONArray extracts the first top-level JSON array from raw LLM
// output, tolerating markdown code fences and surrounding prose.
func ExtractJSONArray(content string) ([]any, error) {
	return extractJSON[[]any](content, '[', ']')
}

func extractJSON[T any](content string, startChar, endChar byte) (T, error) {
	var zero T
	if strings.TrimSpace(content) == "" {
		return zero, NewError("LLM JSON content is empty")
	}
	cleaned := strings.TrimSpace(content)
	cleaned = strings.ReplaceAll(cleaned, "```json", "")
	cleaned = strings.ReplaceAll(cleaned, "```", "")
	cleaned = strings.TrimSpace(cleaned)
	start := strings.IndexByte(cleaned, startChar)
	end := strings.LastIndexByte(cleaned, endChar)
	if start < 0 || end < start {
		return zero, NewError("LLM did not return valid JSON: %s", truncate(content, 200))
	}
	var parsed T
	if err := json.Unmarshal([]byte(cleaned[start:end+1]), &parsed); err != nil {
		return zero, WrapError(err, "LLM JSON parse failed")
	}
	return parsed, nil
}

// PickSlotLabels reads one slot field from an LLM JSON object and keeps only
// labels present in the allowed option list. Values may be arrays or
// separator-joined strings. Matching tolerates case drift ("fat loss" for
// "Fat Loss") and always returns the canonical dictionary spelling, so a
// valid label is never silently dropped; the Java implementation matched
// case-sensitively and lost such labels.
func PickSlotLabels(root map[string]any, field string, allowed []string) []string {
	allowedSet := make(map[string]struct{}, len(allowed))
	canonicalByLower := make(map[string]string, len(allowed))
	for _, value := range allowed {
		allowedSet[value] = struct{}{}
		lower := strings.ToLower(value)
		if _, ok := canonicalByLower[lower]; !ok {
			canonicalByLower[lower] = value
		}
	}
	result := []string{}
	appendIfAllowed := func(value string) {
		for _, candidate := range splitLabelList(value) {
			normalized := strings.TrimSpace(candidate)
			if normalized == "" {
				continue
			}
			canonical, ok := canonicalByLower[strings.ToLower(normalized)]
			if !ok {
				if _, exact := allowedSet[normalized]; !exact {
					continue
				}
				canonical = normalized
			}
			if containsString(result, canonical) {
				continue
			}
			result = append(result, canonical)
		}
	}
	switch node := root[field].(type) {
	case []any:
		for _, item := range node {
			if text, ok := item.(string); ok {
				appendIfAllowed(text)
			}
		}
	case string:
		appendIfAllowed(node)
	}
	return result
}

// labelSeparator splits multi-label strings such as "light, high protein".
// Full-width separators are included because LLMs sometimes emit them even
// in English output, matching the Java SlotJsonPicker behavior.
var labelSeparator = regexp.MustCompile(`\s*[,;，、；/|]\s*`)

func splitLabelList(value string) []string {
	return labelSeparator.Split(value, -1)
}

// jsonFloat reads a decoded JSON value as a float, accepting both numbers
// and numeric strings. LLMs sometimes quote numbers ("0.9"), and the Java
// implementation tolerated that through Jackson's asDouble coercion.
func jsonFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

// jsonInt64 reads a decoded JSON value as an integer, accepting both
// numbers and numeric strings, mirroring Jackson's asLong coercion.
func jsonInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

// jsonText renders a decoded JSON scalar as text the way Jackson's asText
// does: strings pass through, numbers and booleans are stringified, and
// null or containers become an empty string.
func jsonText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	}
	return ""
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsInt64(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// keywordMatcher compiles one word-boundary matcher per keyword so short
// English keywords do not match inside unrelated words.
type keywordMatcher struct {
	patterns []*regexp.Regexp
}

func newKeywordMatcher(keywords ...string) *keywordMatcher {
	matcher := &keywordMatcher{}
	for _, keyword := range keywords {
		quoted := regexp.QuoteMeta(strings.ToLower(strings.TrimSpace(keyword)))
		matcher.patterns = append(matcher.patterns, regexp.MustCompile(`\b`+quoted+`\b`))
	}
	return matcher
}

// matchesAny reports whether text contains any keyword on word boundaries.
// Matching is case-insensitive.
func (m *keywordMatcher) matchesAny(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, pattern := range m.patterns {
		if pattern.MatchString(lower) {
			return true
		}
	}
	return false
}

// jsonString serializes a value for trace payloads; failures degrade to an
// empty JSON object placeholder.
func jsonString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}
