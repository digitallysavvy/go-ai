package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// A caller header that overrides a provider default in another spelling wins
// on the wire, as TS (combineHeaders, then the lower-casing in postToApi's
// withUserAgentSuffix): Config.Headers {"Anthropic-Version": ...} replaces the
// SDK's anthropic-version. Go sent the SDK's value (the lower-case key sorted
// last when the merged map was normalized).
func TestR98carryProviderHeaderOverrideInOtherCase(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		got = r.Header.Get("Anthropic-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()
	m, err := anthropic.New(anthropic.Config{APIKey: "k", BaseURL: srv.URL, Headers: map[string]string{"Anthropic-Version": "2099-01-01"}}).LanguageModel("claude-sonnet-4-5")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	if got != "2099-01-01" {
		t.Fatalf("anthropic-version = %q, want the caller's 2099-01-01", got)
	}
}
