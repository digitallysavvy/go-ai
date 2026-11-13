package middleware

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ruling 4 (round 96): simulated streams keep provider values beyond the
// double range as Infinity and write non-finite metadata values as null.
func TestR96genSimulateStreamingNonFinite(t *testing.T) {
	raw := providerMetadataRawJSON(map[string]interface{}{"p": map[string]interface{}{"x": math.Inf(1)}})
	if string(raw) != `{"p":{"x":null}}` {
		t.Fatalf("raw = %q, want {\"p\":{\"x\":null}}", raw)
	}
	call := toolCallFromCallContent(types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: `{"a":1e400}`, ProviderMetadata: []byte(`{"p":{"x":-1e400}}`)})
	if v, _ := call.Arguments["a"].(float64); !math.IsInf(v, 1) {
		t.Fatalf("arguments = %#v, want a = +Inf", call.Arguments)
	}
	p, _ := call.ProviderMetadata["p"].(map[string]interface{})
	if v, _ := p["x"].(float64); !math.IsInf(v, -1) {
		t.Fatalf("provider metadata = %#v, want p.x = -Inf", call.ProviderMetadata)
	}
}
