package xai

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR97integClientSecretKeepsPrototypeKey: TS doCreateClientSecret reads
// the body with `await response.json()` (xai-realtime-model.ts:76), plain
// JSON.parse, so a __proto__ key is an ordinary property, not a parse
// failure (provg CR, response.json() sites).
func TestR97integClientSecretKeepsPrototypeKey(t *testing.T) {
	srv := r80provbJSONServer(t, `{"value":"ek_1","__proto__":{"x":1},"constructor":{"prototype":{}},"expires_at":5}`)
	res, err := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-realtime").DoCreateClientSecret(t.Context(), provider.ClientSecretOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Token != "ek_1" || res.ExpiresAt == nil || *res.ExpiresAt != 5 {
		t.Fatalf("result = %+v", res)
	}
}

// TestR97integClientSecretIsRawFetch (class sweep of provc M1, plain-fetch
// headers): TS doCreateClientSecret calls fetchFn directly with
// {...config headers, 'Content-Type': 'application/json'}, so the request
// has the provider's user-agent tag but no runtime tag, and a failed request
// rejects as is, not as the retryable "Cannot connect to API" APICallError.
func TestR97integClientSecretIsRawFetch(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, `{"value":"v"}`)
	}))
	if _, err := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-realtime").DoCreateClientSecret(t.Context(), provider.ClientSecretOptions{}); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	if !strings.Contains(ua, "ai-sdk-xai/") || strings.Contains(ua, providerutils.RuntimeEnvironmentUserAgent()) {
		t.Errorf("User-Agent = %q, want the provider tag without the runtime tag", ua)
	}
	_, err := NewRealtimeModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-realtime").DoCreateClientSecret(t.Context(), provider.ClientSecretOptions{})
	if err == nil || strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("err = %v, want the fetch rejection as is", err)
	}
	var retryable interface{ IsRetryable() bool }
	if errors.As(err, &retryable) && retryable.IsRetryable() {
		t.Fatalf("err = %v is retryable; a TS raw fetch rejection is not", err)
	}
}
