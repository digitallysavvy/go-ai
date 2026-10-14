package moonshot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #22045: xhigh and max both map to Moonshot's native "max" effort.
func TestMoonshotReasoningMaxEffort(t *testing.T) {
	for _, l := range []types.ReasoningLevel{types.ReasoningXHigh, types.ReasoningMax} {
		if got := mapReasoningToMoonshotEffort(l); got != "max" {
			t.Fatalf("%s -> %q, want max", l, got)
		}
	}
}
