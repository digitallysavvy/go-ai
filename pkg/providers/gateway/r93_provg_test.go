package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// TS gateway-decision-model.ts sends providerOptions when it is truthy, so a
// caller's empty map is sent as {}.
func TestR93provgDecisionEmptyProviderOptionsForwarded(t *testing.T) {
	for _, tc := range []struct {
		po   map[string]interface{}
		want bool
	}{{map[string]interface{}{}, true}, {nil, false}} {
		var gotBody map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"answers": {}}`))
		}))
		p, err := New(Config{APIKey: "k", BaseURL: server.URL})
		if err != nil {
			t.Fatal(err)
		}
		model, err := p.DecisionModel("typesafe-ai/jev")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := model.DoDecide(t.Context(), provider.DecisionCallOptions{
			State: "s", Questions: map[string]provider.DecisionQuestion{"q": {Type: "boolean", Instructions: "i"}}, ProviderOptions: tc.po,
		}); err != nil {
			t.Fatal(err)
		}
		server.Close()
		if _, ok := gotBody["providerOptions"]; ok != tc.want {
			t.Fatalf("po %#v: providerOptions present = %v", tc.po, ok)
		}
	}
}
