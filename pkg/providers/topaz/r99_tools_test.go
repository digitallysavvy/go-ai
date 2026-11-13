package topaz

import (
	"context"
	"testing"
)

// TS sends frameCount = Math.round(duration * frameRate) as a JS number.
// Go converted it with int(), which is implementation-defined beyond int64
// (it saturates on arm64 and wraps on amd64) and overflows above 2^31-1 on
// 32-bit targets.
func TestR99toolsDerivedFrameCountIsJSNumber(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()

	for _, c := range []struct {
		duration, frameRate, want float64
	}{
		{1e20, 3, 3e20},
		{1e8, 30, 3e9},
	} {
		opts := defaultStartOptions()
		opts.ProviderOptions = map[string]interface{}{
			"topaz": map[string]interface{}{
				"source": map[string]interface{}{"width": 1280, "height": 720, "duration": c.duration, "frameRate": c.frameRate},
			},
		}
		if _, err := ts.model().DoStart(context.Background(), opts); err != nil {
			t.Fatalf("DoStart() error: %v", err)
		}
		source := ts.lastRequest()["source"].(map[string]interface{})
		if source["frameCount"] != c.want {
			t.Errorf("duration %v x frameRate %v: frameCount = %#v, want %v", c.duration, c.frameRate, source["frameCount"], c.want)
		}
	}
}
