package brain

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
)

func TestModelToolsProjection(t *testing.T) {
	original := &mcp.Tool{Name: "home_refresh_screen", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"screen_id": map[string]any{"type": "string"}, "operation_id": map[string]any{"type": "string"}, "wait_ms": map[string]any{"type": "integer"}}, "required": []string{"screen_id", "operation_id"}, "additionalProperties": false}}
	tools, err := ModelTools([]*mcp.Tool{original, {Name: "shell"}, {Name: "home_get_operation"}})
	if err != nil || len(tools) != 1 {
		t.Fatal(tools, err)
	}
	schema := tools[0].InputSchema.(map[string]any)
	props := schema["properties"].(map[string]any)
	if len(props) != 1 || props["screen_id"] == nil {
		t.Fatal(props)
	}
	if len(original.InputSchema.(map[string]any)["properties"].(map[string]any)) != 3 {
		t.Fatal("original mutated")
	}
	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "screen_id" {
		t.Fatal(required)
	}
}

func TestModelToolsRejectUnexpectedProperties(t *testing.T) {
	_, err := ModelTools([]*mcp.Tool{{Name: "home_get_status", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"shell": map[string]any{"type": "string"}}}}})
	if err == nil {
		t.Fatal("unexpected property accepted")
	}
}
