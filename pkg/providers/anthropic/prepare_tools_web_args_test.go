package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// TS validateTypes rejects bad provider-tool args and keeps empty domain
// arrays on the wire.
func TestPrepareTools_WebToolArgsValidatedLikeTS(t *testing.T) {
	bad := []types.Tool{
		tools.WebSearch20260318(tools.WebSearch20260318Config{ResponseInclusion: "nope"}),
		tools.WebFetch20260318(tools.WebFetch20260318Config{ResponseInclusion: "nope"}),
		tools.WebSearch20260209(tools.WebSearch20260209Config{UserLocation: &tools.WebSearchUserLocation{Type: "exact"}}),
		tools.WebSearch20250305(tools.WebSearch20260209Config{UserLocation: &tools.WebSearchUserLocation{}}),
	}
	for _, tl := range bad {
		got := prepareTools(prepareToolsOptions{tools: []types.Tool{tl}})
		if got.err == nil {
			t.Errorf("%s: expected a validation error", tl.Name)
		}
	}
	ok := prepareTools(prepareToolsOptions{tools: []types.Tool{
		tools.WebFetch20260318(tools.WebFetch20260318Config{ResponseInclusion: "excluded", AllowedDomains: []string{}}),
	}})
	if ok.err != nil {
		t.Fatalf("unexpected error: %v", ok.err)
	}
	if v, present := ok.tools[0]["allowed_domains"]; !present || len(v.([]string)) != 0 {
		t.Errorf("allowed_domains = %v (present=%v), want empty array kept", v, present)
	}
}
