package anthropic

import "testing"

// TestBuiltinToolAPIName verifies the exported lookup pkg/providers/bedrock
// uses to forward Anthropic provider-defined tools (e.g. tool_search) through
// a different wire API without duplicating anthropicBuiltinToolTypes.
func TestBuiltinToolAPIName(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"anthropic.tool_search_bm25_20251119", "tool_search_tool_bm25", true},
		{"anthropic.tool_search_regex_20251119", "tool_search_tool_regex", true},
		{"anthropic.bash_20250124", "bash", true},
		{"anthropic.code_execution_20250825", "code_execution", true},
		{"anthropic.web_search_20260209", "", false}, // self-serializing, not a simple builtin
		{"unknown.custom_function_tool", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := BuiltinToolAPIName(tt.name)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("BuiltinToolAPIName(%q) = (%q, %v), want (%q, %v)", tt.name, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
