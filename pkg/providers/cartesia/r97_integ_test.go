package cartesia

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR97integClientSecretKeepsPrototypeKey: TS reads the access token with
// `(await response.json()).token` (cartesia-realtime-model.ts:110), plain
// JSON.parse, so a __proto__ key is an ordinary property.
func TestR97integClientSecretKeepsPrototypeKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"__proto__":{"token":"x"},"token":"t1"}`)
	}))
	defer srv.Close()
	res, err := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "ink-2").DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	if err != nil || res.Token != "t1" {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
}
