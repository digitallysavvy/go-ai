package gateway

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ruling 4: a raw chunk holding 1e400 decodes to +Inf (JSON.parse), as do
// the tool-result value and its provider metadata; an isError tool result
// whose value holds it is JSON.stringify of the value (null).
func TestR96integGatewayOutOfRangeNumbers(t *testing.T) {
	var chunk gatewayStreamChunk
	if err := decodeGatewayStreamChunk([]byte(`{"type":"raw","rawValue":{"x":1e400,"s":"a"}}`), &chunk); err != nil {
		t.Fatal(err)
	}
	s := &gatewayTextStream{includeRawChunks: true}
	part, err, ok := s.convertChunk(&chunk)
	if err != nil || !ok || part.Type != provider.ChunkTypeRaw {
		t.Fatalf("part = %+v, %v, %v", part, err, ok)
	}
	raw, _ := part.Raw.(map[string]interface{})
	if f, _ := raw["x"].(float64); !math.IsInf(f, 1) || raw["s"] != "a" {
		t.Fatalf("raw = %#v, want x=+Inf", part.Raw)
	}

	var p v4Part
	if err := json.Unmarshal([]byte(`{"type":"tool-result","toolCallId":"c","toolName":"t","isError":true,"result":{"x":1e400},"providerMetadata":{"p":{"n":1e400}}}`), &p); err != nil {
		t.Fatal(err)
	}
	tr := p.toolResult()
	if tr.Error == nil || tr.Error.Error() != `{"x":null}` {
		t.Errorf("tool error = %v, want {\"x\":null}", tr.Error)
	}
	md, _ := tr.ProviderMetadata["p"].(map[string]interface{})
	if f, _ := md["n"].(float64); !math.IsInf(f, 1) {
		t.Errorf("provider metadata = %#v, want n=+Inf", tr.ProviderMetadata)
	}
}
