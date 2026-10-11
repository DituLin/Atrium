package brain

import (
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ModelTools clones the trusted home-mcp inventory into the host's model-facing
// contract. Operation identity, polling, and recovery are exclusively host-owned.
// Unknown tools are omitted even when another server advertises them.
func ModelTools(inventory []*mcp.Tool) ([]*mcp.Tool, error) {
	out := []*mcp.Tool{}
	seen := map[string]bool{}
	for _, tool := range inventory {
		if tool == nil {
			continue
		}
		if _, allowed := modelArgumentFields[tool.Name]; !allowed || tool.Name == "home_get_operation" {
			continue
		}

		if seen[tool.Name] {
			return nil, errors.New("duplicate household tool")
		}
		seen[tool.Name] = true
		raw, err := json.Marshal(tool)
		if err != nil {
			return nil, errors.New("invalid household tool definition")
		}
		var clone mcp.Tool
		if json.Unmarshal(raw, &clone) != nil {
			return nil, errors.New("invalid household tool definition")
		}
		schema, ok := clone.InputSchema.(map[string]any)
		if !ok || schema["type"] != "object" {
			return nil, errors.New("invalid household input schema")
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			return nil, errors.New("missing household input properties")
		}
		delete(props, "operation_id")
		delete(props, "wait_ms")
		delete(props, "request_id")
		delete(props, "resolution_key")
		allowed := map[string]bool{}
		for _, key := range modelArgumentFields[tool.Name] {
			allowed[key] = true
		}
		for key := range props {
			if !allowed[key] {
				return nil, errors.New("unexpected household input property")
			}
		}
		required := []any{}
		if values, ok := schema["required"].([]any); ok {
			for _, value := range values {
				if value != "operation_id" && value != "wait_ms" && value != "request_id" && value != "resolution_key" {
					required = append(required, value)
				}
			}
		}
		schema["required"] = required
		schema["additionalProperties"] = false
		if tool.Name == "home_refresh_screen" || tool.Name == "home_show_photo" || tool.Name == "home_navigate_screen" {
			clone.Description = "Request an authorized screen action. The host assigns durable operation identity. accepted means pending; only applied confirms completion. Repeat the same call reference only to query the original outcome."
		}
		out = append(out, &clone)
	}
	return out, nil
}

// CompleteModelTools is the CLI startup gate. It derives the required inventory
// from the same argument allowlist as execution rather than a stale tool count.
func CompleteModelTools(inventory []*mcp.Tool) ([]*mcp.Tool, error) {
	tools, err := ModelTools(inventory)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tool.Name] = true
	}
	for name := range modelArgumentFields {
		if name != "home_get_operation" && !seen[name] {
			return nil, errors.New("incomplete household tool inventory")
		}
	}
	return tools, nil
}
