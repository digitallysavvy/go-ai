package anthropic

import "testing"

// Ports the claude-sonnet-5-5 addition to GoogleVertexAnthropicModelId
// (TS #21631).
func TestClaudeSonnet5_5ModelID(t *testing.T) {
	if string(ClaudeSonnet5_5) != "claude-sonnet-5-5" {
		t.Fatalf("ClaudeSonnet5_5 = %q, want %q", ClaudeSonnet5_5, "claude-sonnet-5-5")
	}
}
