package gemini

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// Ruling 4: a usageMetadata key holding 1e400 is kept in the raw usage as
// +Inf and re-serialized (provider metadata, usage raw) as null.
func TestR96integGeminiUsageOutOfRangeNumber(t *testing.T) {
	var u UsageMetadata
	if err := u.UnmarshalJSON([]byte(`{"promptTokenCount":3,"x":1e400}`)); err != nil {
		t.Fatal(err)
	}
	if f, _ := u.raw["x"].(float64); !math.IsInf(f, 1) || u.PromptTokenCount != 3 {
		t.Fatalf("usage = %+v raw %#v", u, u.raw)
	}
	b, err := jsonstr.Marshal(u)
	if err != nil || string(b) != `{"promptTokenCount":3,"x":null}` {
		t.Fatalf("marshal = %s, %v", b, err)
	}
}
