// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package plan_execute_replan

import (
	"fmt"
	"strings"
)

// extractJSONObject extracts the first complete JSON object from a model
// response that may be wrapped in markdown code fences or surrounded by
// explanatory text / chain-of-thought reasoning.
//
// This mirrors the fallback behavior of Vercel AI SDK's generateObject when a
// provider does not (fully) support response_format schema enforcement: the
// model is instructed via prompt to emit only JSON, but in practice models
// (e.g. Qwen3.7) sometimes wrap output in ```json ... ``` fences or prepend
// reasoning. A naive json.Unmarshal on the raw content then fails.
//
// The function locates the first '{' and returns the balanced substring up to
// the matching '}', correctly handling:
//   - string literals (so braces inside strings do not affect nesting depth)
//   - escaped characters inside strings ('\"', '\\')
//   - comments are NOT supported (JSON does not allow them)
//
// Returns the trimmed JSON substring. If no balanced object is found, the
// original content (trimmed) is returned along with an error so the caller can
// surface the raw model output for debugging.
func extractJSONObject(content string) (string, error) {
	// Strip common markdown code fences: ```json ... ``` or ``` ... ```.
	// Only strip a single fence layer; nested fences are not expected.
	s := strings.TrimSpace(content)
	if strings.HasPrefix(s, "```") {
		// Drop the opening fence line.
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = strings.TrimSpace(s[idx+1:])
		}
		// Drop the closing fence if present.
		if strings.HasSuffix(s, "```") {
			s = strings.TrimSpace(s[:len(s)-3])
		}
	}

	// Find the first '{' to skip any leading prose / reasoning.
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

	// Unbalanced braces — return the trimmed tail for debugging.
	return s[start:], fmt.Errorf("unbalanced braces in model JSON output")
}
