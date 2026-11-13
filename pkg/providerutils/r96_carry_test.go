package providerutils

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// A raw chunk value beyond the double range reads as +Inf (JSON.parse), a
// fractional integer in the same frame still truncates, and the value
// re-serializes as null (JSON.stringify).
func TestR96carryUnmarshalChunkOutOfRangeRawValue(t *testing.T) {
	var chunk struct {
		Index int                    `json:"index"`
		Raw   map[string]interface{} `json:"raw"`
	}
	if err := UnmarshalChunk([]byte(`{"index":2.0,"raw":{"x":1e400,"y":[-1e400]}}`), &chunk); err != nil {
		t.Fatalf("UnmarshalChunk: %v", err)
	}
	if chunk.Index != 2 {
		t.Errorf("index = %d", chunk.Index)
	}
	if f, _ := chunk.Raw["x"].(float64); !math.IsInf(f, 1) {
		t.Errorf("raw.x = %#v", chunk.Raw["x"])
	}
	out, err := jsonstr.Marshal(chunk.Raw)
	if err != nil || string(out) != `{"x":null,"y":[null]}` {
		t.Errorf("re-serialized = %s, %v", out, err)
	}
}
