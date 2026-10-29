package contracts

import (
	"reflect"
	"testing"
)

// TestParseProjectInput covers the three valid input formats of ParseProjectInput.
//
// Regression background: {"input":"plain text"} once failed to parse (json: cannot unmarshal string into
// map[string]interface{}) — when input is a plain-text string, after the double-JSON parse failed
// it fell through to the final object parse and errored, making the whole pre_create chain return a failure retry card.
func TestParseProjectInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want map[string]any
	}{
		{
			name: "plain-text input (previously failing)",
			raw:  `{"input":"夏日旅行混剪"}`,
			want: map[string]any{"input": "夏日旅行混剪"},
		},
		{
			name: "double-encoded JSON string",
			raw:  `{"input":"{\"input\":\"夏日旅行混剪\"}"}`,
			want: map[string]any{"input": "夏日旅行混剪"},
		},
		{
			name: "inner JSON object",
			raw:  `{"input":{"input":"夏日旅行混剪"}}`,
			want: map[string]any{"input": "夏日旅行混剪"},
		},
		{
			name: "empty input",
			raw:  `{"input":""}`,
			want: map[string]any{},
		},
		{
			name: "empty string",
			raw:  ``,
			want: map[string]any{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseProjectInput(tc.raw)
			if err != nil {
				t.Fatalf("ParseProjectInput(%q) error: %v", tc.raw, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseProjectInput(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
