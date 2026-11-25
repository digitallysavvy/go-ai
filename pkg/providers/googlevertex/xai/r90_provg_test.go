package xai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR90provgProviderIDAndUserAgent: TS createGoogleVertexXai builds on
// createOpenAICompatible({name: 'googleVertex.xai'}): chat models report
// `${name}.chat`, and requests carry the ai-sdk-openai-compatible user agent
// suffix (openai-compatible-provider.ts getHeaders).
func TestR90provgProviderIDAndUserAgent(t *testing.T) {
	var ua string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","created":1,"model":"grok-4","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	p, _ := New(Config{BaseURL: server.URL, AccessToken: "t"})
	m, err := p.LanguageModel("grok-4")
	if err != nil {
		t.Fatal(err)
	}
	if m.Provider() != "googleVertex.xai.chat" {
		t.Fatalf("Provider() = %q, want googleVertex.xai.chat", m.Provider())
	}
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ua, "ai-sdk-openai-compatible/") {
		t.Fatalf("User-Agent = %q, want the ai-sdk-openai-compatible suffix", ua)
	}
	restored, err := provider.DeserializeModel(provider.SerializedModel{Provider: "googleVertex.xai", ModelID: "grok-4", Config: map[string]interface{}{"BaseURL": server.URL}})
	if err != nil || restored.Provider() != "googleVertex.xai.chat" {
		t.Fatalf("v0.5 payload restore: %v %v", restored, err)
	}
}
