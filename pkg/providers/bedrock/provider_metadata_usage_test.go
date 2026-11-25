package bedrock

import (
	"reflect"
	"testing"
)

// usage is a required field of the Converse response, so TS always emits
// providerMetadata.{amazonBedrock,bedrock} = { stopSequence: null } at least.
func TestConverseResponseAlwaysCarriesProviderMetadata(t *testing.T) {
	m := newBedrockModelWithID("amazon.nova-pro-v1:0")
	r, err := m.convertConverseResponse([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"hi"}]}},"stopReason":"end_turn","usage":{"inputTokens":1,"outputTokens":2,"totalTokens":3}}`), false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"stopSequence": nil}
	for _, key := range []string{"amazonBedrock", "bedrock"} {
		if got := r.ProviderMetadata[key]; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s metadata = %#v, want %#v", key, got, want)
		}
	}
}
