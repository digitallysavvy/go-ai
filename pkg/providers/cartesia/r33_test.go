package cartesia

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Number("4.41e4") is the integer 44100 and Number(" 24000 ") is 24000.
func TestR33_OutputFormatSampleRateUsesJSNumber(t *testing.T) {
	for in, want := range map[string]int{"mp3_4.41e4": 44100, "wav_ 24000 ": 24000, "pcm_0x5dc0": 24000} {
		var warnings []types.Warning
		got := resolveOutputFormat(in, nil, &warnings)
		if got["sample_rate"] != want || len(warnings) != 0 {
			t.Errorf("%s: sample_rate=%v warnings=%v", in, got["sample_rate"], warnings)
		}
	}
	var warnings []types.Warning
	resolveOutputFormat("mp3_44100.5", nil, &warnings)
	if len(warnings) != 1 {
		t.Errorf("fractional rate warnings = %v", warnings)
	}
}
