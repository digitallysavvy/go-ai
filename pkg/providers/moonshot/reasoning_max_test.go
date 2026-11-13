package moonshot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS #22045 / 61a315e333: max maps to Moonshot's native "max"; xhigh now
// rounds down to "high", medium to "low".
func TestMoonshotReasoningMaxEffort(t *testing.T) {
	for level, want := range map[types.ReasoningLevel]string{
		types.ReasoningMax: "max", types.ReasoningXHigh: "high", types.ReasoningHigh: "high",
		types.ReasoningMedium: "low", types.ReasoningLow: "low", types.ReasoningMinimal: "low", types.ReasoningNone: "low",
	} {
		level := level
		if got, _ := providerutils.MapReasoningToProviderEffort(&level, moonshotEffortMap, nil, nil); got != want {
			t.Errorf("%s -> %q, want %s", level, got, want)
		}
	}
}
