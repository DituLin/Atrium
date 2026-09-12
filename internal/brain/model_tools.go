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
		switch tool.Name {
		case "home_get_status", "home_list_screens", "home_get_screen", "home_get_nas_status", "home_list_photos", "home_get_photo", "home_get_command", "home_refresh_screen", "home_show_photo", "home_navigate_screen":
		default:
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
				if value != "operation_id" && value != "wait_ms" {
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
