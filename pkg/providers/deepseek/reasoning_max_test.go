package deepseek

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #22045: max maps to DeepSeek's native "max" effort.
func TestDeepSeekTopLevelReasoningMax(t *testing.T) {
	got, ok := deepseekTopLevelReasoningEffort(types.ReasoningMax)
	if !ok || got != "max" {
		t.Fatalf("got %q ok=%v, want max", got, ok)
	}
}
