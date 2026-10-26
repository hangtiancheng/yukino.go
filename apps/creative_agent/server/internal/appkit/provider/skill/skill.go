package appkitskill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	appkittools "github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/appkit/provider/tools"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/react"

	"github.com/goccy/go-yaml"
)

// ============ §7.7 SkillProvider — SKILL.md 动态工具源 ============
// SkillProvider 从 skills 目录扫描 SKILL.md 文件夹，把其中的 API 定义
// 动态暴露给 LLM。相比 ToolRegistry（编译期 Register），Skill 是"懒加载"：
// 只有 LLM 调用 skill_load 显式加载某个 skill 后，其 API 才进入工具列表。
//
// 一个 skill 是一个含 YAML frontmatter 的 SKILL.md：
//
//	---
//	name: weather
//	description: 查询城市天气。当用户询问天气、气温、降雨时调用。
//	apis:
//	  - name: query_weather
//	    description: 查询指定城市的当前天气
//	    endpoint: ${WEATHER_API_BASE_URL}/api/weather
//	    method: GET
//	    parameters:
//	      city:
//	        type: string
//	        description: 城市名（中文）
//	        required: true
//	      user_id:
//	        type: string
//	        required: true
//	        injected: true
//	---
//	# weather 使用说明
//	...
//
// 关键点：
//   - frontmatter 中的 ${VAR} 会被环境变量 / 显式传入的 vars 展开；
//   - injected: true 的参数由服务端注入（不要求 LLM 提供），如 user_id。

// APIDefinition 描述 skill 中的一个可调用 API。
type APIDefinition struct {
	Name        string              `yaml:"name" json:"name"`
	Description string              `yaml:"description" json:"description,omitempty"`
	Endpoint    string              `yaml:"endpoint" json:"endpoint"`
	Method      string              `yaml:"method" json:"method"`
	Headers     map[string]string   `yaml:"headers,omitempty" json:"headers,omitempty"`
	Parameters  map[string]APIParam `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}

// APIParam 描述 API 的单个参数。
type APIParam struct {
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description" json:"description,omitempty"`
	Required    bool   `yaml:"required" json:"required,omitempty"`
	Default     any    `yaml:"default,omitempty" json:"default,omitempty"`
	Enum        []any  `yaml:"enum,omitempty" json:"enum,omitempty"`
	Injected    bool   `yaml:"injected" json:"injected,omitempty"`
}

// SkillSummary 是 skill 的轻量摘要（列表/展示用，不含 Body）。
type SkillSummary struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	APIs        []APIDefinition `json:"apis,omitempty"`
}

// Skill 是一个完整的 skill（frontmatter 摘要 + markdown 正文）。
type Skill struct {
	Summary SkillSummary
	Body    string
}

// skillFrontmatter 是 SKILL.md 的 YAML frontmatter。
type skillFrontmatter struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	APIs        []APIDefinition `yaml:"apis"`
}

// SkillRepository 提供 skill 的扫描与读取能力。
// FSRepository 是文件夹实现；测试可用内存实现替换。
type SkillRepository interface {
	Summaries() []SkillSummary
	Get(name string) (*Skill, error)
	GetWithVars(name string, vars map[string]string) (*Skill, error)
}

// FSRepository 扫描一个或多个 skills 根目录，以 skill name 为索引。
type FSRepository struct {
	index map[string]string // skill name → 所在目录路径
	vars  map[string]string
}

// NewFSRepository 扫描 roots 下所有 SKILL.md，建立 name → 目录 索引。
func NewFSRepository(roots ...string) (*FSRepository, error) {
	r := &FSRepository{index: map[string]string{}}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || d.Name() != "SKILL.md" {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fm, _, ok := splitYAMLFrontMatter(string(b))
			if !ok || fm.Name == "" {
				return fmt.Errorf("skill %s: frontmatter 缺 name", p)
			}
			r.index[fm.Name] = filepath.Dir(p)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Summaries 返回所有 skill 的摘要（每次实时读盘，方便热更新）。
func (r *FSRepository) Summaries() []SkillSummary {
	var out []SkillSummary
	for _, dir := range r.index {
		b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil {
			continue
		}
		fm, _, ok := splitYAMLFrontMatter(string(b))
		if !ok {
			continue
		}
		out = append(out, SkillSummary{Name: fm.Name, Description: fm.Description, APIs: fm.APIs})
	}
	return out
}

// Get 读取单个 skill（不带额外变量）。
func (r *FSRepository) Get(name string) (*Skill, error) {
	return r.GetWithVars(name, nil)
}

// GetWithVars 读取单个 skill，并展开 frontmatter 中的 ${VAR}（优先 vars，其次环境变量）。
func (r *FSRepository) GetWithVars(name string, vars map[string]string) (*Skill, error) {
	dir, ok := r.index[name]
	if !ok {
		return nil, fmt.Errorf("skill %q not found", name)
	}
	b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	text := expandVars(string(b), vars)
	fm, body, ok := splitYAMLFrontMatter(text)
	if !ok {
		return nil, fmt.Errorf("skill %q: 无 frontmatter", name)
	}
	return &Skill{
		Summary: SkillSummary{Name: fm.Name, Description: fm.Description, APIs: fm.APIs},
		Body:    body,
	}, nil
}

// splitYAMLFrontMatter 把 SKILL.md 拆成 frontmatter 与正文。
// 仅当文件以 `---` 开头且在第二行以内找到闭合 `---` 时认为有 frontmatter。
func splitYAMLFrontMatter(text string) (skillFrontmatter, string, bool) {
	var fm skillFrontmatter
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return fm, "", false
	}
	var end int
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == 0 {
		return fm, "", false
	}
	block := strings.Join(lines[1:end], "\n")
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return fm, "", false
	}
	body := strings.TrimPrefix(strings.Join(lines[end+1:], "\n"), "\n")
	return fm, body, true
}

// expandVars 展开字符串中的 ${KEY}：优先用 vars，其次读环境变量。
func expandVars(text string, vars map[string]string) string {
	return os.Expand(text, func(key string) string {
		if v, ok := vars[key]; ok {
			return v
		}
		return os.Getenv(key)
	})
}

// ============ RemoteSkillExecutor — 通用 HTTP 执行器 ============

// RemoteSkillExecutor 把 APIDefinition 转换为通用 HTTP 调用。
// GET/DELETE → query string；POST/PUT → JSON body；请求体 30s 超时；
// 响应 >=400 视为错误。
type RemoteSkillExecutor struct {
	client *http.Client
}

// NewRemoteSkillExecutor 创建执行器；client 为 nil 时用 http.DefaultClient。
func NewRemoteSkillExecutor(client *http.Client) *RemoteSkillExecutor {
	if client == nil {
		client = http.DefaultClient
	}
	return &RemoteSkillExecutor{client: client}
}

// Execute 调用远程 API 并返回响应体文本。
func (e *RemoteSkillExecutor) Execute(ctx context.Context, api APIDefinition, params map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	method := strings.ToUpper(api.Method)
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	values := url.Values{}
	switch method {
	case http.MethodGet, http.MethodDelete, http.MethodHead:
		for k, v := range params {
			values.Set(k, paramString(v))
		}
	default: // POST / PUT / PATCH → JSON body
		b, err := json.Marshal(params)
		if err != nil {
			return "", fmt.Errorf("marshal body: %w", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, api.Endpoint, body)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if method != http.MethodGet && method != http.MethodDelete && method != http.MethodHead {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range api.Headers {
		req.Header.Set(k, v)
	}
	if values.Encode() != "" {
		req.URL.RawQuery = values.Encode()
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// paramString 把参数值序列化为查询串值。
func paramString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}

// ============ SkillProvider — ToolsProvider 实现 ============

// APIMapping 记录某个 API 属于哪个 skill（卸载/查找用）。
type APIMapping struct {
	SkillName string
	APIDef    APIDefinition
}

// SkillProvider 实现 ToolsProvider：skill_load 懒加载 + 已加载 skill 的 API 暴露。
type SkillProvider struct {
	repo     SkillRepository
	executor *RemoteSkillExecutor

	mu       sync.RWMutex
	loaded   map[string]bool
	apiIndex map[string]APIMapping
}

// NewSkillProvider 创建 SkillProvider。
func NewSkillProvider(repo SkillRepository, executor *RemoteSkillExecutor) *SkillProvider {
	if executor == nil {
		executor = NewRemoteSkillExecutor(nil)
	}
	return &SkillProvider{
		repo:     repo,
		executor: executor,
		loaded:   map[string]bool{},
		apiIndex: map[string]APIMapping{},
	}
}

// GetFunctionDefs 返回 skill_load（内建）与所有已加载 skill 的 API Schema。
// skill 参数带 Enum 枚举所有可用 skill 名。
func (p *SkillProvider) GetFunctionDefs(ctx context.Context) ([]react.FunctionDef, error) {
	defs := []react.FunctionDef{
		{
			Name:        "skill_load",
			Description: "加载一个 skill 到工具列表，返回加载成功的 skill 名。",
			Parameters: appkittools.ParameterSpec{
				Type: "object",
				Properties: map[string]appkittools.PropertyDef{
					"skill": {
						Type:        "string",
						Description: "要加载的 skill 名",
						Enum:        p.availableSkillNames(),
					},
				},
				Required: []string{"skill"},
			},
		},
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, mapping := range p.apiIndex {
		defs = append(defs, apiToFunctionDef(mapping.APIDef))
	}
	return defs, nil
}

// Execute 执行工具。skill_load 由本 provider 处理；其余按 apiIndex 查表调用执行器。
func (p *SkillProvider) Execute(ctx context.Context, name, args string) (string, bool, bool, error) {
	if name == "skill_load" {
		return p.handleSkillLoad(ctx, args)
	}

	p.mu.RLock()
	mapping, ok := p.apiIndex[name]
	p.mu.RUnlock()
	if !ok {
		return "", false, false, fmt.Errorf("unknown tool: %s", name)
	}

	params := appkittools.ParseArgs(args)

	// 注入 injected: true 的参数（由服务端提供，LLM 不传）
	for k, param := range mapping.APIDef.Parameters {
		if !param.Injected {
			continue
		}
		if _, exists := params[k]; exists {
			continue
		}
		params[k] = injectedValue(param)
	}

	result, err := p.executor.Execute(ctx, mapping.APIDef, params)
	if err != nil {
		return "", true, false, err
	}
	return result, true, false, nil
}

// injectedValue 计算 injected 参数的取值（目前以参数 Default 兜底）。
func injectedValue(param APIParam) any {
	if param.Default != nil {
		return param.Default
	}
	return ""
}

// handleSkillLoad 处理 skill_load 工具调用。
func (p *SkillProvider) handleSkillLoad(ctx context.Context, args string) (string, bool, bool, error) {
	params := appkittools.ParseArgs(args)
	name := appkittools.ArgString(params, "skill", "")
	if name == "" {
		return "", true, false, fmt.Errorf("skill_load: 缺少 skill 参数")
	}

	skill, err := p.repo.GetWithVars(name, nil)
	if err != nil {
		return "", true, false, err
	}

	p.mu.Lock()
	p.loaded[name] = true
	for i := range skill.Summary.APIs {
		api := skill.Summary.APIs[i]
		p.apiIndex[api.Name] = APIMapping{SkillName: name, APIDef: api}
	}
	p.mu.Unlock()

	return fmt.Sprintf("loaded skill: %s", name), true, false, nil
}

// availableSkillNames 返回所有可加载的 skill 名（供 skill_load 的 Enum）。
func (p *SkillProvider) availableSkillNames() []string {
	summaries := p.repo.Summaries()
	names := make([]string, 0, len(summaries))
	for _, s := range summaries {
		names = append(names, s.Name)
	}
	return names
}

// apiToFunctionDef 把 APIDefinition 转为 FunctionDef（跳过 injected 参数，enum 转 []string）。
func apiToFunctionDef(api APIDefinition) appkittools.FunctionDef {
	props := map[string]appkittools.PropertyDef{}
	required := []string{}
	for name, param := range api.Parameters {
		if param.Injected {
			continue // 服务端注入的参数不暴露给 LLM
		}
		props[name] = appkittools.PropertyDef{
			Type:        param.Type,
			Description: param.Description,
			Enum:        enumToStrings(param.Enum),
		}
		if param.Required {
			required = append(required, name)
		}
	}
	return appkittools.FunctionDef{
		Name:        api.Name,
		Description: api.Description,
		Parameters: appkittools.ParameterSpec{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
	}
}

// enumToStrings 把 []any 枚举转成 []string（JSON Schema enum 需字符串数组）。
func enumToStrings(enum []any) []string {
	if len(enum) == 0 {
		return nil
	}
	out := make([]string, 0, len(enum))
	for _, v := range enum {
		out = append(out, fmt.Sprintf("%v", v))
	}
	return out
}
