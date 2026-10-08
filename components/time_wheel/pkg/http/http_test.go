package http

import "testing"

func Test_getCompleteURL(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		params map[string]string
		want   string
	}{
		{"no params", "http://x.com/api", nil, "http://x.com/api"},
		{"empty params", "http://x.com/api", map[string]string{}, "http://x.com/api"},
		{"simple params", "http://x.com/api", map[string]string{"a": "1", "b": "2"}, "http://x.com/api?a=1&b=2"},
		// A value with a space must stay escaped ("+"-encoded), not be
		// unescaped back into a raw space inside the URL.
		{"params needing escaping", "http://x.com/api", map[string]string{"q": "hello world"}, "http://x.com/api?q=hello+world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getCompleteURL(tt.origin, tt.params); got != tt.want {
				t.Fatalf("getCompleteURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
