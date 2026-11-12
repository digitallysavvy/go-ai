package gateway

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r93provgBody(t *testing.T, po map[string]interface{}) map[string]interface{} {
	t.Helper()
	m := newTestGatewayLanguageModel("openai/gpt-5")
	body, err := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ProviderOptions: po}, false)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// TS getArgs forwards the call's providerOptions as given: the Gateway, not
// the client, lets gateway.serviceTier override a provider's own tier
// option, so openai.serviceTier stays in the body.
func TestR93provgServiceTierNotStripped(t *testing.T) {
	body := r93provgBody(t, map[string]interface{}{
		"gateway": map[string]interface{}{"serviceTier": "flex"},
		"openai":  map[string]interface{}{"serviceTier": "priority", "service_tier": "priority"},
	})
	want := map[string]interface{}{
		"gateway": map[string]interface{}{"serviceTier": "flex"},
		"openai":  map[string]interface{}{"serviceTier": "priority", "service_tier": "priority"},
	}
	if !reflect.DeepEqual(body["providerOptions"], want) {
		t.Fatalf("providerOptions = %#v", body["providerOptions"])
	}
}

// TS sends providerOptions: {} for a caller's empty map and leaves the key
// out only when the caller gave none ('should work without provider options').
func TestR93provgEmptyProviderOptionsForwarded(t *testing.T) {
	body := r93provgBody(t, map[string]interface{}{})
	if po, ok := body["providerOptions"].(map[string]interface{}); !ok || len(po) != 0 {
		t.Fatalf("empty providerOptions = %#v (present %v)", body["providerOptions"], ok)
	}
	if _, ok := r93provgBody(t, nil)["providerOptions"]; ok {
		t.Fatal("nil providerOptions should be omitted")
	}
}
