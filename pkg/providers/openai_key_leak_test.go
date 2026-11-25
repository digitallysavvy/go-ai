package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/baseten"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
	"github.com/digitallysavvy/go-ai/pkg/providers/vercel"
)

// Wrapper providers built on openai.New must never send OPENAI_API_KEY to
// their own service when they have no key of their own.
func TestWrapperProvidersDoNotSendOpenAIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-openai-leak")
	for _, env := range []string{"CEREBRAS_API_KEY", "DEEPINFRA_API_KEY", "BASETEN_API_KEY", "VERCEL_API_KEY", "GOOGLE_VERTEX_ACCESS_TOKEN"} {
		t.Setenv(env, "")
	}

	var mu sync.Mutex
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	type lm interface {
		LanguageModel(string) (provider.LanguageModel, error)
	}
	cases := map[string]lm{
		"cerebras":  cerebras.New(cerebras.Config{BaseURL: srv.URL}),
		"deepinfra": deepinfra.New(deepinfra.Config{BaseURL: srv.URL}),
		"baseten":   baseten.New(baseten.Config{BaseURL: srv.URL}),
		"vercel":    vercel.New(vercel.Config{BaseURL: srv.URL}),
		"vertex maas": googlevertex.NewMaaS(googlevertex.MaaSConfig{
			Project: "p", Location: "us-central1", BaseURL: srv.URL,
			AuthToken: func(context.Context) (string, error) { return "", nil },
		}),
	}
	for name, p := range cases {
		mu.Lock()
		auth = nil
		mu.Unlock()
		model, err := p.LanguageModel("m")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_, genErr := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		mu.Lock()
		got := append([]string(nil), auth...)
		mu.Unlock()
		// v0.6.0 fails a keyless call with a LoadAPIKeyError before sending
		// anything (TS loadApiKey); v0.5.3 sent it without a key.
		if len(got) == 0 && !providererrors.IsLoadAPIKeyError(genErr) {
			t.Errorf("%s: no request and no LoadAPIKeyError: %v", name, genErr)
		}
		for _, a := range got {
			if strings.Contains(a, "sk-openai-leak") {
				t.Errorf("%s sent the OpenAI key: %q", name, a)
			}
		}
	}
}

// A wrapper still reads its own environment variable.
func TestWrapperProvidersReadOwnKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-openai-leak")
	t.Setenv("CEREBRAS_API_KEY", "csk")
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	model, _ := cerebras.New(cerebras.Config{BaseURL: srv.URL}).LanguageModel("m")
	_, _ = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if got != "Bearer csk" {
		t.Fatalf("Authorization = %q", got)
	}
}
