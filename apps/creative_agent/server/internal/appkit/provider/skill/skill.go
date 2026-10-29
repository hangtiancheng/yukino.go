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

// ============ §7.7 SkillProvider — SKILL.md dynamic tool source ============
// SkillProvider scans the skills directory for SKILL.md folders and dynamically
// exposes their API definitions to the LLM. Unlike ToolRegistry (compile-time Register), skills are "lazy-loaded":
// a skill's APIs enter the tool list only after the LLM explicitly loads it via skill_load.
//
// A skill is a SKILL.md with YAML frontmatter:
//
//	---
//	name: weather
//	description: Query city weather. Call when the user asks about weather, temperature, or rain.
//	apis:
//	  - name: query_weather
//	    description: Query the current weather of the given city
//	    endpoint: ${WEATHER_API_BASE_URL}/api/weather
//	    method: GET
//	    parameters:
//	      city:
//	        type: string
//	        description: City name (in Chinese, e.g. 北京)
//	        required: true
//	      user_id:
//	        type: string
//	        required: true
//	        injected: true
//	---
//	# weather skill usage
//	...
//
// Key points:
//   - ${VAR} in the frontmatter is expanded from env vars / explicitly passed vars;
//   - parameters with injected: true are injected server-side (not required from the LLM), e.g. user_id.

// APIDefinition describes one callable API in a skill.
type APIDefinition struct {
	Name        string              `yaml:"name" json:"name"`
	Description string              `yaml:"description" json:"description,omitempty"`
	Endpoint    string              `yaml:"endpoint" json:"endpoint"`
	Method      string              `yaml:"method" json:"method"`
	Headers     map[string]string   `yaml:"headers,omitempty" json:"headers,omitempty"`
	Parameters  map[string]APIParam `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}

// APIParam describes a single parameter of an API.
type APIParam struct {
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description" json:"description,omitempty"`
	Required    bool   `yaml:"required" json:"required,omitempty"`
	Default     any    `yaml:"default,omitempty" json:"default,omitempty"`
	Enum        []any  `yaml:"enum,omitempty" json:"enum,omitempty"`
	Injected    bool   `yaml:"injected" json:"injected,omitempty"`
}

// SkillSummary is a lightweight summary of a skill (for listing/display, without the Body).
type SkillSummary struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	APIs        []APIDefinition `json:"apis,omitempty"`
}

// Skill is a complete skill (frontmatter summary + markdown body).
type Skill struct {
	Summary SkillSummary
	Body    string
}

// skillFrontmatter is the YAML frontmatter of a SKILL.md.
type skillFrontmatter struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	APIs        []APIDefinition `yaml:"apis"`
}

// SkillRepository provides skill scanning and reading.
// FSRepository is the folder implementation; tests can swap in an in-memory implementation.
type SkillRepository interface {
	Summaries() []SkillSummary
	Get(name string) (*Skill, error)
	GetWithVars(name string, vars map[string]string) (*Skill, error)
}

// FSRepository scans one or more skills root directories, indexed by skill name.
type FSRepository struct {
	index map[string]string // skill name → directory path
	vars  map[string]string
}

// NewFSRepository scans all SKILL.md files under roots and builds the name → directory index.
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
				return fmt.Errorf("skill %s: frontmatter missing name", p)
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

// Summaries returns the summaries of all skills (read from disk each time, enabling hot updates).
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

// Get reads a single skill (without extra variables).
func (r *FSRepository) Get(name string) (*Skill, error) {
	return r.GetWithVars(name, nil)
}

// GetWithVars reads a single skill and expands ${VAR} in the frontmatter (vars first, then env vars).
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
		return nil, fmt.Errorf("skill %q: no frontmatter", name)
	}
	return &Skill{
		Summary: SkillSummary{Name: fm.Name, Description: fm.Description, APIs: fm.APIs},
		Body:    body,
	}, nil
}

// splitYAMLFrontMatter splits a SKILL.md into frontmatter and body.
// Frontmatter is recognized only when the file starts with `---` and a closing `---` is found within the second line.
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

// expandVars expands ${KEY} in a string: vars first, then env vars.
func expandVars(text string, vars map[string]string) string {
	return os.Expand(text, func(key string) string {
		if v, ok := vars[key]; ok {
			return v
		}
		return os.Getenv(key)
	})
}

// ============ RemoteSkillExecutor — generic HTTP executor ============

// RemoteSkillExecutor turns an APIDefinition into a generic HTTP call.
// GET/DELETE → query string; POST/PUT → JSON body; 30s request timeout;
// responses >=400 are treated as errors.
type RemoteSkillExecutor struct {
	client *http.Client
}

// NewRemoteSkillExecutor creates the executor; http.DefaultClient is used when client is nil.
func NewRemoteSkillExecutor(client *http.Client) *RemoteSkillExecutor {
	if client == nil {
		client = http.DefaultClient
	}
	return &RemoteSkillExecutor{client: client}
}

// Execute calls the remote API and returns the response body text.
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

// paramString serializes a parameter value into a query-string value.
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

// ============ SkillProvider — ToolsProvider implementation ============

// APIMapping records which skill an API belongs to (for unloading/lookup).
type APIMapping struct {
	SkillName string
	APIDef    APIDefinition
}

// SkillProvider implements ToolsProvider: skill_load lazy loading + API exposure of loaded skills.
type SkillProvider struct {
	repo     SkillRepository
	executor *RemoteSkillExecutor

	mu       sync.RWMutex
	loaded   map[string]bool
	apiIndex map[string]APIMapping
}

// NewSkillProvider creates a SkillProvider.
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

// GetFunctionDefs returns the schema of skill_load (built-in) plus the APIs of all loaded skills.
// The skill parameter carries an Enum listing all available skill names.
func (p *SkillProvider) GetFunctionDefs(ctx context.Context) ([]react.FunctionDef, error) {
	defs := []react.FunctionDef{
		{
			Name:        "skill_load",
			Description: "Load a skill into the tool list; returns the name of the successfully loaded skill.",
			Parameters: appkittools.ParameterSpec{
				Type: "object",
				Properties: map[string]appkittools.PropertyDef{
					"skill": {
						Type:        "string",
						Description: "Name of the skill to load",
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

// Execute runs a tool. skill_load is handled by this provider; the rest are looked up in apiIndex and delegated to the executor.
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

	// Inject parameters marked injected: true (provided server-side, not by the LLM)
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

// injectedValue computes the value of an injected parameter (currently falling back to the parameter Default).
func injectedValue(param APIParam) any {
	if param.Default != nil {
		return param.Default
	}
	return ""
}

// handleSkillLoad handles the skill_load tool call.
func (p *SkillProvider) handleSkillLoad(ctx context.Context, args string) (string, bool, bool, error) {
	params := appkittools.ParseArgs(args)
	name := appkittools.ArgString(params, "skill", "")
	if name == "" {
		return "", true, false, fmt.Errorf("skill_load: missing skill parameter")
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

// availableSkillNames returns all loadable skill names (for the skill_load Enum).
func (p *SkillProvider) availableSkillNames() []string {
	summaries := p.repo.Summaries()
	names := make([]string, 0, len(summaries))
	for _, s := range summaries {
		names = append(names, s.Name)
	}
	return names
}

// apiToFunctionDef converts an APIDefinition into a FunctionDef (skipping injected parameters, converting enum to []string).
func apiToFunctionDef(api APIDefinition) appkittools.FunctionDef {
	props := map[string]appkittools.PropertyDef{}
	required := []string{}
	for name, param := range api.Parameters {
		if param.Injected {
			continue // server-injected parameters are not exposed to the LLM
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

// enumToStrings converts a []any enum into []string (JSON Schema enum requires a string array).
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
