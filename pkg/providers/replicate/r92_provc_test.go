package replicate

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR92provcNaNDurationIsFalsy (r92 provc-13): TS `if (options.duration)`
// skips a NaN duration.
func TestR92provcNaNDurationIsFalsy(t *testing.T) {
	nan := math.NaN()
	input, err := (&VideoModel{modelID: "luma/ray"}).buildInput(&provider.VideoModelV3CallOptions{Prompt: "p", Duration: &nan})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := input["duration"]; ok {
		t.Errorf("input = %v", input)
	}
}
