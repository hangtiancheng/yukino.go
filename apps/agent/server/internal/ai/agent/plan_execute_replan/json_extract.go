package plan_execute_replan

import (
	"fmt"
	"strings"
)

func extractJSONObject(content string) (string, error) {
	s := strings.TrimSpace(content)
	if strings.HasPrefix(s, "```") {
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = strings.TrimSpace(s[idx+1:])
		}
		if strings.HasSuffix(s, "```") {
			s = strings.TrimSpace(s[:len(s)-3])
		}
	}

	start := strings.IndexByte(s, '{')
	if start < 0 {
		return content, fmt.Errorf("no JSON object found in model output")
	}

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1], nil
			}
		}
	}

	return s[start:], fmt.Errorf("unbalanced braces in model JSON output")
}
