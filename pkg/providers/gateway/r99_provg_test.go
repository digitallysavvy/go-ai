package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS createAuthHeaders spreads options.headers after the protocol version and
// team headers, and withUserAgentSuffix lower-cases the names in entry order,
// so a custom header in another case replaces the SDK's. Go copied the
// custom headers beside the SDK keys and the sorted lower-casing kept the
// SDK's lower-case value.
func TestR99provgCustomHeaderInOtherCaseWins(t *testing.T) {
	var mu sync.Mutex
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[],"finishReason":"stop","usage":{}}`))
	}))
	defer server.Close()

	p, err := New(Config{
		APIKey: "test-key", BaseURL: server.URL, TeamIDOrSlug: "team_config",
		Headers: map[string]string{
			"AI-Gateway-Protocol-Version": "9.9.9",
			"X-Vercel-AI-Gateway-Team":    "team_custom",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := p.LanguageModel("openai/gpt-4o")
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if v := got.Values("Ai-Gateway-Protocol-Version"); len(v) != 1 || v[0] != "9.9.9" {
		t.Fatalf("ai-gateway-protocol-version = %q, want the custom header", v)
	}
	if v := got.Values("X-Vercel-Ai-Gateway-Team"); len(v) != 1 || v[0] != "team_custom" {
		t.Fatalf("x-vercel-ai-gateway-team = %q, want the custom header", v)
	}
}

// TS postJsonToApi sends {'Content-Type': 'application/json', ...headers}:
// a custom Content-Type replaces the JSON one on the client secret mint.
func TestR99provgMintClientSecretCustomContentTypeWins(t *testing.T) {
	var contentType []string
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Values("Content-Type")
		_, _ = w.Write([]byte(`{"token":"vcst_test"}`))
	}))
	defer closeServer()
	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai", Headers: map[string]string{"content-type": "application/vnd.custom+json"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{ModelID: "openai/gpt-realtime"}); err != nil {
		t.Fatal(err)
	}
	if len(contentType) != 1 || contentType[0] != "application/vnd.custom+json" {
		t.Fatalf("content-type = %q, want the custom header", contentType)
	}
}
