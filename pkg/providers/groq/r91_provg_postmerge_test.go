package groq

// Post-merge test (provg, round 91, added by the integrator after the provb
// merge): provb2a-11 makes providerutils mergeProviderOptions read typed
// literals through NormalizeOptionsJSON. Fails before that fix only because
// of the helper.

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

// Item 7 (provb2a-11 dependency): Go-typed groq options reach the body like
// their JSON form; validated by the shared openai-compatible helper.
func TestR91provgPostMergeTypedOptionLiterals(t *testing.T) {
	var got []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer server.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).LanguageModel("llama-3.3-70b-versatile")
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"groq": map[string]string{"user": "u1", "serviceTier": "flex"}},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"user":"u1"`) || !strings.Contains(string(got), `"service_tier":"flex"`) {
		t.Fatalf("body = %s", got)
	}
}
