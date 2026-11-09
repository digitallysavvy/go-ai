package anthropic

import "testing"

// TS 84208104a4 google-vertex-anthropic-provider.test.ts: the native-output
// opt-in is supported for claude-fable-5-2.
func TestR90provaFable52NativeOutput(t *testing.T) {
	if ClaudeFable5_2 != "claude-fable-5-2" || !vertexAnthropicSupportsNativeStructuredOutput(string(ClaudeFable5_2)) {
		t.Fatal("claude-fable-5-2 must support the native-output opt-in")
	}
}
