package speechstream

import (
	"math"
	"testing"
)

// A finite positive rate passes isAudioFormat (TS keeps the JS number); a
// rate beyond int must not wrap to a negative value.
func TestR95carryAudioRateBeyondIntIsClamped(t *testing.T) {
	for _, rate := range []string{"1e20", "4294967296"} {
		part, ok := ParsePart(`{"type":"audio","audio":"AAA=","audioFormat":{"type":"audio/pcm","rate":` + rate + `}}`)
		if !ok || part.AudioFormat.Rate == nil {
			t.Fatalf("rate %s: part not parsed", rate)
		}
		if got := *part.AudioFormat.Rate; got <= 0 || (rate == "1e20" && got != math.MaxInt) {
			t.Errorf("rate %s: Rate = %d", rate, got)
		}
	}
}
