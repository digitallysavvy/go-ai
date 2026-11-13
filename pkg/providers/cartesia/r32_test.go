package cartesia

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS splits "mp3_" into ["mp3", ""]: the empty sample rate is present, is not
// a valid rate, and warns.
func TestResolveOutputFormatTrailingUnderscoreWarns(t *testing.T) {
	var warnings []types.Warning
	got := resolveOutputFormat("mp3_", nil, &warnings)
	if got["sample_rate"] != 44100 {
		t.Fatalf("format = %v", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Details, `Unsupported Cartesia sample rate in output format "mp3_". Using 44100 Hz instead.`) {
		t.Fatalf("warnings = %+v", warnings)
	}
}
