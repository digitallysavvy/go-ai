package bytedance

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR92provcNaNDurationIsFalsy (r92 provc-13): TS `if (options.duration)`
// skips a NaN duration; Go sent it and JSON encoding failed.
func TestR92provcNaNDurationIsFalsy(t *testing.T) {
	model := newVideoModel(&Provider{}, "seedance-1-0-pro-250528")
	nan := math.NaN()
	body, err := model.buildRequestBody(&provider.VideoModelV3CallOptions{Prompt: "p", Duration: &nan}, &ProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["duration"]; ok {
		t.Errorf("body = %v", body)
	}
}
