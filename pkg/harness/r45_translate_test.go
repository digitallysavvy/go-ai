package harness

import "testing"

// TS `toolCall?.providerExecuted ?? isProviderExecuted?.(id) ?? true`: a
// failed tool-result whose call leaves providerExecuted undefined asks
// IsProviderExecuted (or defaults to provider-executed), while an explicit
// false keeps it a host tool-result.
func TestR45TranslateToolErrorFallsBackWhenCallLeavesProviderExecutedUndefined(t *testing.T) {
	for name, tc := range map[string]struct {
		info      ToolCallInfo
		isPE      func(string) bool
		wantError bool
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
			if got := chunks[0].ToolResult.Error != nil; got != tc.wantError {
				t.Fatalf("tool-error = %v, want %v (%+v)", got, tc.wantError, chunks[0].ToolResult)
			}
		})
	}
}
