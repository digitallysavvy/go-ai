package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS gateway-provider.ts mintClientSecret posts with postJsonToApi:
// createAuthHeaders' ai-sdk-gateway tag, then the runtime tag.
func TestR98provgMintClientSecretCarriesRuntimeUserAgent(t *testing.T) {
	var ua string
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"token":"vcst_test"}`))
	}))
	defer closeServer()
	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{ModelID: "openai/gpt-realtime"}); err != nil {
		t.Fatal(err)
	}
	rt := providerutils.RuntimeEnvironmentUserAgent()
	if !strings.HasPrefix(ua, "ai-sdk-gateway/") || !strings.HasSuffix(ua, " "+rt) {
		t.Fatalf("user-agent = %q, want ai-sdk-gateway/... %s", ua, rt)
	}
}

// TS mintClientSecret body: {model, routeKind?, expiresIn?} in that order.
func TestR98provgMintClientSecretBodyKeyOrder(t *testing.T) {
	var body string
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"token":"vcst_test"}`))
	}))
	defer closeServer()
	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatal(err)
	}
	expires := 60
	if _, err := p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{ModelID: "m", RouteKind: "transcription", ExpiresAfterSeconds: &expires}); err != nil {
		t.Fatal(err)
	}
	if want := `{"model":"m","routeKind":"transcription","expiresIn":60}`; body != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
}
