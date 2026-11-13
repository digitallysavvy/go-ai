package bedrock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS combineHeaders(getHeaders(), options.headers) spreads the call headers
// after the provider headers, and postJsonToApi's withUserAgentSuffix then
// lower-cases the keys in insertion order, so a call header wins over a
// provider header of the same name whatever the spelling of either.
func TestR99provaCallHeaderWinsOverConfigHeaderAnyCase(t *testing.T) {
	var mu sync.Mutex
	got := map[string][]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got[r.URL.Path] = append(got[r.URL.Path], r.Header.Values("X-Custom")...)
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"stop"}`))
	}))
	defer srv.Close()
	for _, tc := range []struct{ configKey, callKey string }{
		{"x-custom", "X-Custom"},
		{"X-Custom", "x-custom"},
		{"X-CUSTOM", "x-Custom"},
	} {
		mu.Lock()
		got = map[string][]string{}
		mu.Unlock()
		p := New(Config{AWSAccessKeyID: "AKID", AWSSecretAccessKey: "secret-key", Region: "us-east-1", BaseURL: srv.URL,
			Headers: map[string]string{tc.configKey: "config"}})
		call := map[string]string{tc.callKey: "call"}
		ctx := context.Background()
		lm, _ := p.LanguageModel("amazon.nova-lite-v1:0")
		_, _ = lm.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Headers: call})
		if s, err := lm.DoStream(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Headers: call}); err == nil {
			_ = s.Close()
		}
		em, _ := p.EmbeddingModel("amazon.titan-embed-text-v2:0")
		_, _ = em.DoEmbed(ctx, "hi", &provider.EmbedModelOptions{Headers: call})
		im, _ := p.ImageModel("amazon.nova-canvas-v1:0")
		_, _ = im.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "a cat", Headers: call})
		rm, _ := p.RerankingModel("amazon.rerank-v1:0")
		_, _ = rm.DoRerank(ctx, &provider.RerankOptions{Query: "q", Documents: []string{"a"}, Headers: call})

		mu.Lock()
		if len(got) < 4 {
			t.Fatalf("%v: requests = %v", tc, got)
		}
		for path, values := range got {
			for _, v := range values {
				if v != "call" {
					t.Errorf("config %q / call %q: %s X-Custom = %v, want only the call value", tc.configKey, tc.callKey, path, values)
					break
				}
			}
		}
		mu.Unlock()
	}
}

// TS getArgs: on a model that rejects disabled thinking, reasoning none falls
// back to adaptive thinking with effort 'low' only when
// additionalModelRequestFields.output_config.effort is null
// (z.record(z.string(), z.any())): any other value, a number included, keeps
// the caller's effort.
func TestR99provaAdaptiveFallbackKeepsNonStringEffort(t *testing.T) {
	none := types.ReasoningNone
	args := r85provaArgs(t, "anthropic.claude-opus-5-5", &provider.GenerateOptions{
		Reasoning: &none,
		ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{
			"additionalModelRequestFields": map[string]interface{}{"output_config": map[string]interface{}{"effort": 5}},
		}},
	})
	fields := r85provaFields(args)
	oc, _ := fields["output_config"].(map[string]interface{})
	if oc["effort"] != float64(5) {
		t.Fatalf("output_config = %#v, want the caller's effort 5", fields["output_config"])
	}
	if th, _ := fields["thinking"].(map[string]interface{}); th["type"] != "adaptive" {
		t.Fatalf("thinking = %#v", fields["thinking"])
	}
}
