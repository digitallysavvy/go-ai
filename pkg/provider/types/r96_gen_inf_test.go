package types

import (
	"math"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// Ruling 4 (round 96): a provider number beyond the double range decodes as
// Infinity (TS JSON.parse) and serializes as null (TS JSON.stringify), also
// through a type with its own MarshalJSON.
func TestR96genNonFiniteProviderValues(t *testing.T) {
	got := toolCallContentInput(ToolCallContent{Input: `{"a":1e400}`})
	m, ok := got.(map[string]interface{})
	if !ok || !math.IsInf(m["a"].(float64), 1) {
		t.Fatalf("input = %#v, want a = +Inf", got)
	}
	data, err := jsonstr.Marshal(SpeechResult{ProviderMetadata: map[string]interface{}{"p": map[string]interface{}{"x": math.Inf(1), "y": math.NaN()}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"providerMetadata":{"p":{"x":null,"y":null}}`) {
		t.Fatalf("json = %s, want non-finite values as null", data)
	}
}
