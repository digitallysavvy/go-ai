package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS add01abd2c: the OpenAI-compatible chat model writes verbosity after the
// passthrough only when textVerbosity is set, so a raw verbosity survives.
func TestR87provbCompatRawVerbosityPassthrough(t *testing.T) {
	for _, tc := range []struct {
		opts map[string]interface{}
		want interface{}
	}{
		{map[string]interface{}{"verbosity": "low"}, "low"},
		{map[string]interface{}{"verbosity": "low", "textVerbosity": "high"}, "high"},
		{map[string]interface{}{}, nil},
	} {
		var body map[string]interface{}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"x"},"finish_reason":"stop"}]}`))
		}))
		p := New(Config{APIKey: "k", BaseURL: srv.URL, OpenAICompatible: true, Name: "test-provider", ChatProviderName: "test-provider.chat"})
		m, _ := p.ChatModel("gpt-5")
		_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:          chatTestPrompt(),
			ProviderOptions: map[string]interface{}{"test-provider": tc.opts},
		})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if body["verbosity"] != tc.want {
			t.Errorf("%v: verbosity = %v, want %v", tc.opts, body["verbosity"], tc.want)
		}
	}
}
