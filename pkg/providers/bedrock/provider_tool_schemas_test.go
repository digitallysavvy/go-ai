package bedrock

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	anthropictools "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// wireSchemaJSON renders a toolSpec inputSchema.json value as it goes on the
// wire.
func wireSchemaJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	return string(b)
}

// TestAnthropicProviderToolSchemasMatchTSZod pins the schema Bedrock sends for
// Anthropic provider tools to what TS derives from the zod input schema of
// the tool factory (key order included), not the Go tool's own Parameters.
func TestAnthropicProviderToolSchemasMatchTSZod(t *testing.T) {
	const draft = `{"$schema":"http://json-schema.org/draft-07/schema#",`
	tests := []struct {
		tool types.Tool
		want string
	}{
		{anthropictools.Bash20250124(), draft + `"type":"object","properties":{"command":{"type":"string"},"restart":{"type":"boolean"}},"required":["command"],"additionalProperties":false}`},
		{anthropictools.ToolSearchRegex20251119(), draft + `"type":"object","properties":{"pattern":{"type":"string"},"limit":{"type":"number"}},"required":["pattern"],"additionalProperties":false}`},
		{anthropictools.WebFetch20250910(anthropictools.WebFetch20260209Config{}), draft + `"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`},
		{anthropictools.WebSearch20260209(anthropictools.WebSearch20260209Config{}), draft + `"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`},
	}
	for _, tt := range tests {
		t.Run(tt.tool.Name, func(t *testing.T) {
			spec := bedrockAnthropicProviderTool(tt.tool)
			if spec == nil {
				t.Fatalf("no toolSpec for %s", tt.tool.Name)
			}
			schema := spec["inputSchema"].(map[string]interface{})["json"]
			if got := wireSchemaJSON(t, schema); got != tt.want {
				t.Fatalf("inputSchema.json =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// TestAnthropicProviderToolSchemaTableCoversEveryTool fails when a provider
// tool the Go SDK ships has no TS zod schema in the embedded table.
func TestAnthropicProviderToolSchemaTableCoversEveryTool(t *testing.T) {
	ids := []string{
		"anthropic.advisor_20260301", "anthropic.bash_20241022", "anthropic.bash_20250124",
		"anthropic.code_execution_20250522", "anthropic.code_execution_20250825", "anthropic.code_execution_20260120",
		"anthropic.computer_20241022", "anthropic.computer_20250124", "anthropic.computer_20251124",
		"anthropic.computer_toolset_20260801", "anthropic.memory_20250818",
		"anthropic.text_editor_20241022", "anthropic.text_editor_20250124", "anthropic.text_editor_20250429", "anthropic.text_editor_20250728",
		"anthropic.web_fetch_20250910", "anthropic.web_fetch_20260209", "anthropic.web_fetch_20260318",
		"anthropic.web_search_20250305", "anthropic.web_search_20260209", "anthropic.web_search_20260318",
		"anthropic.tool_search_regex_20251119", "anthropic.tool_search_bm25_20251119",
	}
	for _, id := range ids {
		raw, ok := anthropicProviderToolSchema(id)
		if !ok {
			t.Errorf("no TS schema for %s", id)
			continue
		}
		if !json.Valid(raw) || !strings.HasPrefix(string(raw), `{"$schema":"http://json-schema.org/draft-07/schema#"`) {
			t.Errorf("%s: invalid schema %s", id, raw)
		}
	}
}

func TestBedrockReplayedToolInputKeepsKeyOrderAndLegacyToolCalls(t *testing.T) {
	const raw = `{"zeta":1,"alpha":{"yy":1,"aa":2}}`
	args := map[string]interface{}{"zeta": 1, "alpha": map[string]interface{}{"yy": 1, "aa": 2}}
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: raw, Arguments: args},
		}, ToolCalls: []types.ToolCall{{ID: "c2", ToolName: "t", RawArguments: raw, Arguments: args}}},
	}
	_, out, err := convertToBedrockChatMessages(msgs, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out) //nolint:errcheck // test
	if got := strings.Count(string(b), `"input":`+raw); got != 2 {
		t.Fatalf("want 2 ordered tool inputs (content part + legacy ToolCalls), got %d in %s", got, b)
	}
}
