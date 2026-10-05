package bridge

import (
	"crypto/sha256"
	"fmt"
)

type ToolSpec struct{ Name, Namespace, Kind string }
type Tools struct {
	Definitions []any
	Specs       map[string]ToolSpec
	Compaction  bool
}

func FlatName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	s := namespace + "__" + name
	if len(s) <= 64 {
		return s
	}
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%.46s__%x", s, h[:8])
}
func BuildTools(body Object) (*Tools, error) {
	t := &Tools{Definitions: []any{}, Specs: map[string]ToolSpec{}}
	var add func(Object, string) error
	add = func(tool Object, ns string) error {
		kind := Str(tool["type"])
		if kind == "namespace" {
			children := List(tool["tools"])
			if children == nil {
				children = List(tool["children"])
			}
			for _, child := range children {
				if err := add(Obj(child), Str(tool["name"])); err != nil {
					return err
				}
			}
			return nil
		}
		if kind == "web_search" || kind == "web_search_preview" {
			return nil
		}
		if kind != "function" && kind != "custom" && kind != "tool_search" {
			return fmt.Errorf("unsupported tool type %q", kind)
		}
		name := Str(tool["name"])
		function := tool
		if nested := Obj(tool["function"]); nested != nil {
			function = nested
			name = Str(nested["name"])
		}
		if kind == "tool_search" {
			name = "tool_search"
		}
		if name == "" {
			return fmt.Errorf("tool name must not be empty")
		}
		flat := FlatName(ns, name)
		spec := ToolSpec{Name: name, Namespace: ns, Kind: kind}
		if old, exists := t.Specs[flat]; exists {
			if old != spec {
				return fmt.Errorf("tool names collide after flattening")
			}
			return nil
		}
		schema := function["parameters"]
		if schema == nil {
			schema = Object{"type": "object", "properties": Object{}}
		}
		description := Str(function["description"])
		if kind == "custom" {
			schema = Object{"type": "object", "properties": Object{"input": Object{"type": "string"}}, "required": []string{"input"}}
			description += "\nPass the original tool's raw input unchanged in the input string. Original tool definition: " + JSON(tool)
		}
		if kind == "tool_search" {
			schema = Object{"type": "object", "properties": Object{"query": Object{"type": "string"}}, "required": []string{"query"}}
			description = "Search and load Codex tools and MCP namespaces."
		}
		definition := Object{"type": "function", "name": flat, "description": description, "parameters": schema}
		if strict, ok := function["strict"]; ok {
			definition["strict"] = strict
		}
		t.Definitions = append(t.Definitions, definition)
		t.Specs[flat] = spec
		return nil
	}
	for _, raw := range List(body["tools"]) {
		if err := add(Obj(raw), ""); err != nil {
			return nil, err
		}
	}
	for _, raw := range InputItems(body["input"]) {
		item := Obj(raw)
		if item["type"] == "compaction_trigger" {
			t.Compaction = true
		}
		if item["type"] == "tool_search_output" || item["type"] == "additional_tools" {
			for _, raw := range List(item["tools"]) {
				if err := add(Obj(raw), ""); err != nil {
					return nil, err
				}
			}
		}
	}
	return t, nil
}
func (t *Tools) Call(id, name, args, status string) Object {
	spec, ok := t.Specs[name]
	if !ok {
		spec = ToolSpec{Name: name, Kind: "function"}
	}
	item := Object{"id": "fc_" + id, "type": "function_call", "call_id": id, "name": spec.Name, "arguments": args, "status": status}
	if spec.Namespace != "" {
		item["namespace"] = spec.Namespace
	}
	switch spec.Kind {
	case "custom":
		input, _ := Decode([]byte(args))
		item["id"] = "ctc_" + id
		item["type"] = "custom_tool_call"
		item["input"] = Str(input["input"])
		delete(item, "arguments")
	case "tool_search":
		input, _ := Decode([]byte(args))
		item["type"] = "tool_search_call"
		item["execution"] = "client"
		item["arguments"] = input
		delete(item, "name")
	}
	return item
}
func InputItems(value any) []any {
	if s, ok := value.(string); ok {
		return []any{Object{"type": "message", "role": "user", "content": s}}
	}
	if item := Obj(value); item != nil {
		return []any{item}
	}
	return List(value)
}
