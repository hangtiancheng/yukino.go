package contracts

import (
	"reflect"
	"testing"
)

// TestParseProjectInput 覆盖 ParseProjectInput 的三种合法输入格式。
//
// 回归背景：{"input":"纯文本"} 曾解析失败（json: cannot unmarshal string into
// map[string]interface{}）——input 是纯文本字符串时，二次 JSON 解析失败后
// 直接落到最后的对象解析而报错，导致 pre_create 全链路返回失败重试卡。
func TestParseProjectInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want map[string]any
	}{
		{
			name: "纯文本 input（曾失败）",
			raw:  `{"input":"夏日旅行混剪"}`,
			want: map[string]any{"input": "夏日旅行混剪"},
		},
		{
			name: "二次 JSON 字符串",
			raw:  `{"input":"{\"input\":\"夏日旅行混剪\"}"}`,
			want: map[string]any{"input": "夏日旅行混剪"},
		},
		{
			name: "内层 JSON 对象",
			raw:  `{"input":{"input":"夏日旅行混剪"}}`,
			want: map[string]any{"input": "夏日旅行混剪"},
		},
		{
			name: "空 input",
			raw:  `{"input":""}`,
			want: map[string]any{},
		},
		{
			name: "空字符串",
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
