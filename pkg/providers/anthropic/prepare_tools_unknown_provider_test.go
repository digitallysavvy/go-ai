package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS prepareTools: an unknown provider tool id yields exactly one
// `provider-defined tool <id>` warning and is not sent.
func TestPrepareToolsUnknownAnthropicProviderToolWarnsOnce(t *testing.T) {
	for _, tl := range []types.Tool{
		{Name: "anthropic.future_20990101"},
		{Name: "x", Type: "provider", ProviderID: "anthropic.future_20990101"},
	} {
		got := prepareTools(prepareToolsOptions{tools: []types.Tool{tl}})
		if len(got.tools) != 0 || len(got.warnings) != 1 ||
			got.warnings[0].Type != "unsupported" || got.warnings[0].Feature != "provider-defined tool anthropic.future_20990101" {
			t.Fatalf("tool %+v: tools=%v warnings=%+v", tl, got.tools, got.warnings)
		}
	}
}
