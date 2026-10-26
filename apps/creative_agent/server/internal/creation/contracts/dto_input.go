package contracts

import (
	"encoding/json"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// 通用约定：所有接口均为 HTTP POST，JSON body，响应 200 + JSON，业务成败靠 errno 区分。

// Input 是"翻译器"接口：把用户输入 + 上一阶段产物翻译成下游算子（special skill）要求的入参。
//
// 这是配置驱动可扩展性的核心：
//   - AuditTexts 从这个阶段的输入里提取待风控的文本组
//   - InputFill 把用户输入 + 上一阶段产物组装成算子的 feat_id + base64 参数
//
// 加一个新创作类型，只需在 PreCreationInputMap 里配一条 + 实现本接口，不改业务代码。
type Input interface {
	// AuditTexts 返回待风控的文本组（每组一段文本，便于按组判定/合并审计记录）。
	AuditTexts() [][]string
	// InputFill 把用户输入翻译成下游算子入参。
	// project 可能为 nil（编剧/点子王等轻量路径第一次进入时无 project）。
	InputFill(project *domain.Project, req *PreCreateReq, logID string) (any, error)
}

// CommonInput 是各阶段输入的通用实现，阶段 1 先做"透传原始输入"的兜底翻译。
//
// 阶段 3 起，各创作类型可实现自己的 Input（AuditTexts 按内容提取文本组、InputFill 组装算子入参），
// 未实现自定义类型的阶段先复用本实现保证链路可跑通。
type CommonInput struct {
	// Params 是外层 input 解包后的具体参数。
	Params map[string]any `json:"-"`
}

// ParseProjectInput 解析外层包裹：{"input":"<具体参数 JSON>"}，input 本身可能是 JSON 字符串或对象。
func ParseProjectInput(raw string) (map[string]any, error) {
	if raw == "" {
		return map[string]any{}, nil
	}
	var outer struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal([]byte(raw), &outer); err != nil {
		return nil, err
	}
	if len(outer.Input) == 0 {
		return map[string]any{}, nil
	}
	// input 可能再包一层 JSON 字符串（用户侧二次序列化）。
	var inner string
	if err := json.Unmarshal(outer.Input, &inner); err == nil {
		var params map[string]any
		if inner != "" {
			if err := json.Unmarshal([]byte(inner), &params); err == nil {
				return params, nil
			}
			// input 是纯文本（如 "夏日旅行混剪"）：不是二次 JSON 对象，
			// 把它作为普通字符串参数返回，而不是报错（{"input":"纯文本"} 是合法契约）。
			return map[string]any{"input": inner}, nil
		}
		// input 是空字符串：返回空参数（{"input":""} 合法）。
		return map[string]any{}, nil
	}
	var params map[string]any
	if err := json.Unmarshal(outer.Input, &params); err != nil {
		return nil, err
	}
	return params, nil
}

// ParseInput 解析原始 project_input 字符串并返回 Input 翻译器。
func ParseInput(raw string) (Input, error) {
	params, err := ParseProjectInput(raw)
	if err != nil {
		return nil, err
	}
	// 阶段 3 起：根据 (role, type) 从 PreCreationInputMap.NewInput() 取具体类型并填充。
	return &CommonInput{Params: params}, nil
}

// AuditTexts 通用实现：把所有文本类字段收集为待审文本组（按单字段一组）。
// 具体创作类型阶段 3 起可覆盖。
func (c *CommonInput) AuditTexts() [][]string {
	var groups [][]string
	for _, v := range c.Params {
		if s, ok := v.(string); ok && s != "" {
			groups = append(groups, []string{s})
		}
	}
	return groups
}

// InputFill 通用实现：阶段 1 直接把解析后的参数作为下游算子入参（透传）。
// 阶段 3/4 起，各阶段会从 project.Ext.StepOutput 捞上一阶段产物组装 feat_id + base64 参数。
func (c *CommonInput) InputFill(project *domain.Project, req *PreCreateReq, logID string) (any, error) {
	return c.Params, nil
}
