package heygen

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// heygen-video-model.test.ts (TS 6f41ccc1f8): long durations and
// ultraquality pass through to HeyGen, which enforces its own limits.
func TestR85provaHeyGenLongDurationAndUltraquality(t *testing.T) {
	for _, d := range []float64{1, 16, 30, 31, 60} {
		for _, pe := range []string{"quality", "turbo", "disabled", "ultraquality"} {
			ts := newTestServer(t)
			d := d
			_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
				o.Duration = &d
				o.ProviderOptions = heygenOpts(map[string]interface{}{"promptEnhancement": pe})
			}))
			if err != nil {
				t.Fatalf("duration %v %s: %v", d, pe, err)
			}
			body := ts.call(0).body
			if body["duration"] != d || body["prompt_enhancement"] != pe {
				t.Errorf("duration %v %s: body = %v", d, pe, body)
			}
		}
	}
	ts := newTestServer(t)
	_, err := newModel(ts).DoStart(context.Background(), opts(func(o *provider.VideoModelV3CallOptions) {
		d := 5.5
		o.Duration = &d
	}))
	if err == nil || err.Error() == "" || ts.callCount() != 0 {
		t.Fatalf("fractional: %v", err)
	}
}
