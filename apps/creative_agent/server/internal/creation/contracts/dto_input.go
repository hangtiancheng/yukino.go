package contracts

import (
	"encoding/json"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"
)

// General convention: all endpoints are HTTP POST with JSON bodies; responses are 200 + JSON, with business success/failure distinguished by errno.

// Input is the "translator" interface: it translates user input + the previous stage's product into the inputs required by downstream operators (special skills).
//
// This is the core of config-driven extensibility:
//   - AuditTexts extracts the text groups to audit from this stage's input
//   - InputFill assembles user input + the previous stage's product into the operator's feat_id + base64 params
//
// Adding a new creation type only requires one entry in PreCreationInputMap + implementing this interface, with no business-code changes.
type Input interface {
	// AuditTexts returns the text groups to audit (one text per group, for per-group decisions / merged audit records).
	AuditTexts() [][]string
	// InputFill translates user input into downstream operator inputs.
	// project may be nil (lightweight paths like scriptwriter/idea-king have no project on first entry).
	InputFill(project *domain.Project, req *PreCreateReq, logID string) (any, error)
}

// CommonInput is the generic implementation of stage inputs; stage 1 starts with the "pass through the raw input" fallback translation.
//
// From stage 3, each creation type may implement its own Input (AuditTexts extracting text groups by content, InputFill assembling operator inputs);
// stages without custom types reuse this implementation so the chain keeps working.
type CommonInput struct {
	// Params holds the concrete parameters after unwrapping the outer input.
	Params map[string]any `json:"-"`
}

// ParseProjectInput parses the outer wrapper: {"input":"<concrete params JSON>"}, where input itself may be a JSON string or an object.
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
	// input may be wrapped in another JSON string layer (client-side double serialization).
	var inner string
	if err := json.Unmarshal(outer.Input, &inner); err == nil {
		var params map[string]any
		if inner != "" {
			if err := json.Unmarshal([]byte(inner), &params); err == nil {
				return params, nil
			}
			// input is plain text (e.g. "夏日旅行混剪"): not a doubly-encoded JSON object;
			// return it as an ordinary string parameter instead of erroring ({"input":"plain text"} is a valid contract).
			return map[string]any{"input": inner}, nil
		}
		// input is an empty string: return empty params ({"input":""} is valid).
		return map[string]any{}, nil
	}
	var params map[string]any
	if err := json.Unmarshal(outer.Input, &params); err != nil {
		return nil, err
	}
	return params, nil
}

// ParseInput parses the raw project_input string and returns the Input translator.
func ParseInput(raw string) (Input, error) {
	params, err := ParseProjectInput(raw)
	if err != nil {
		return nil, err
	}
	// From stage 3: fetch the concrete type from PreCreationInputMap.NewInput() by (role, type) and fill it.
	return &CommonInput{Params: params}, nil
}

// AuditTexts generic implementation: collect all text-like fields into audit text groups (one group per field).
// Concrete creation types may override this from stage 3.
func (c *CommonInput) AuditTexts() [][]string {
	var groups [][]string
	for _, v := range c.Params {
		if s, ok := v.(string); ok && s != "" {
			groups = append(groups, []string{s})
		}
	}
	return groups
}

// InputFill generic implementation: stage 1 passes the parsed params directly as downstream operator inputs (pass-through).
// From stages 3/4, each stage fetches the previous stage's product from project.Ext.StepOutput to assemble feat_id + base64 params.
func (c *CommonInput) InputFill(project *domain.Project, req *PreCreateReq, logID string) (any, error) {
	return c.Params, nil
}
