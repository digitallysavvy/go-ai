package openai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR97integClientSecretKeepsPrototypeKey: TS doCreateClientSecret reads
// the body with `await response.json()` (openai-realtime-model.ts:89), plain
// JSON.parse, so a __proto__ key is an ordinary property, not a parse
// failure (provg CR, response.json() sites).
func TestR97integClientSecretKeepsPrototypeKey(t *testing.T) {
	srv := r80provbJSONServer(t, `{"value":"ek_1","__proto__":{"x":1},"constructor":{"prototype":{}},"expires_at":5}`)
	res, err := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-realtime").DoCreateClientSecret(t.Context(), provider.ClientSecretOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "ek_1" || res.ExpiresAt == nil || *res.ExpiresAt != 5 {
		t.Fatalf("result = %+v", res)
	}
}
