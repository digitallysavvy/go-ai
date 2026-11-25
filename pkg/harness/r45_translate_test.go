package harness

import "testing"

// TS `toolCall?.providerExecuted ?? isProviderExecuted?.(id) ?? true`: a
// failed tool-result whose call leaves providerExecuted undefined asks
// IsProviderExecuted (or defaults to provider-executed), while an explicit
// false marks the tool-error as a host tool's (TS 7fffff9f02: every failed
// result is a tool-error).
func TestR45TranslateToolErrorFallsBackWhenCallLeavesProviderExecutedUndefined(t *testing.T) {
	for name, tc := range map[string]struct {
		info                 ToolCallInfo
		isPE                 func(string) bool
		wantProviderExecuted bool
	}{
		"undefined, no callback":    {ToolCallInfo{}, nil, true},
		"undefined, callback true":  {ToolCallInfo{}, func(string) bool { return true }, true},
		"undefined, callback false": {ToolCallInfo{}, func(string) bool { return false }, false},
		"explicit false":            {ToolCallInfo{ProviderExecutedSet: true}, func(string) bool { return true }, false},
		"true":                      {ToolCallInfo{ProviderExecuted: true}, func(string) bool { return false }, true},
	} {
		t.Run(name, func(t *testing.T) {
			chunks := TranslatePart(&ToolResultPart{ToolCallID: "c1", ToolName: "echo", Result: "boom", IsError: true}, TranslateOptions{
				GetToolCall:        func(string) (ToolCallInfo, bool) { return tc.info, true },
				IsProviderExecuted: tc.isPE,
			})
			if len(chunks) != 1 || chunks[0].ToolResult == nil {
				t.Fatalf("chunks = %+v", chunks)
			}
			if chunks[0].ToolResult.Error == nil {
				t.Fatalf("not a tool-error: %+v", chunks[0].ToolResult)
			}
			if got := chunks[0].ToolResult.ProviderExecuted; got != tc.wantProviderExecuted || !chunks[0].ToolResult.ProviderExecutedSet {
				t.Fatalf("providerExecuted = %v, want %v (%+v)", got, tc.wantProviderExecuted, chunks[0].ToolResult)
			}
		})
	}
}
