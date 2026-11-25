package ai

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// cr-r99-gen-to-integrator: the tool-call part's providerMetadata is an
// object in TS (no parse), so 1e400 is Infinity and the metadata stays; the
// raw input parses with safeParseJSON (a __proto__ key fails it).
func TestR99integApprovalToolCallMetadataAndInput(t *testing.T) {
	tc := toolCallFromContentPart(types.ToolCallContent{
		ToolCallID:       "c",
		ToolName:         "t",
		Input:            `{"__proto__":{"x":1}}`,
		ProviderMetadata: json.RawMessage(`{"p":{"n":1e400,"s":"v"}}`),
	})
	p, _ := tc.ProviderMetadata["p"].(map[string]interface{})
	if n, _ := p["n"].(float64); !math.IsInf(n, 1) || p["s"] != "v" {
		t.Errorf("ProviderMetadata = %#v, want p.n +Inf and p.s kept", tc.ProviderMetadata)
	}
	if tc.Arguments != nil {
		t.Errorf("Arguments = %#v, want nil (safeParseJSON rejects __proto__)", tc.Arguments)
	}
}
