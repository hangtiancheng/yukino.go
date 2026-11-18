package tools

import (
	"context"
	"encoding/json"
	"fmt"

	sdktools "github.com/hangtiancheng/yukino-code/apps/yukino/tools"
	reflectschema "github.com/invopop/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type functionTool struct {
	name, description string
	category          sdktools.ToolCategory
	schema            map[string]any
	validator         *jsonschema.Schema
	run               func(context.Context, map[string]any) (string, error)
}

func (t *functionTool) Name() string                    { return t.name }
func (t *functionTool) Description() string             { return t.description }
func (t *functionTool) Category() sdktools.ToolCategory { return t.category }
func (t *functionTool) Schema() map[string]any {
	return map[string]any{"name": t.name, "description": t.description, "input_schema": t.schema}
}
func (t *functionTool) Execute(ctx context.Context, args map[string]any) sdktools.ToolResult {
	if args == nil {
		args = map[string]any{}
	}
	if err := ctx.Err(); err != nil {
		return sdktools.ToolResult{Output: err.Error(), IsError: true}
	}
	if err := t.validator.Validate(args); err != nil {
		return sdktools.ToolResult{Output: err.Error(), IsError: true}
	}
	out, err := t.run(ctx, args)
	if err != nil {
		return sdktools.ToolResult{Output: err.Error(), IsError: true}
	}
	return sdktools.ToolResult{Output: out}
}
func newFunction[T any](name, description string, category sdktools.ToolCategory, run func(context.Context, *T) (string, error)) (sdktools.Tool, error) {
	reflector := &reflectschema.Reflector{DoNotReference: true}
	raw, err := json.Marshal(reflector.Reflect(new(T)))
	if err != nil {
		return nil, err
	}
	var schema map[string]any
	if err = json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err = compiler.AddResource("urn:tool:"+name, schema); err != nil {
		return nil, err
	}
	validator, err := compiler.Compile("urn:tool:" + name)
	if err != nil {
		return nil, fmt.Errorf("compile %s schema: %w", name, err)
	}
	return &functionTool{name: name, description: description, category: category, schema: schema, validator: validator, run: func(ctx context.Context, args map[string]any) (string, error) {
		raw, err := json.Marshal(args)
		if err != nil {
			return "", err
		}
		var input T
		if err = json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
		return run(ctx, &input)
	}}, nil
}
