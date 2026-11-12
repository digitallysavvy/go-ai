package providers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// r91provbCapture runs call against a server that records the JSON body of
// the first request and answers 400, and returns that body (nil when no
// request was sent).
func r91provbCapture(t *testing.T, call func(base string)) interface{} {
	t.Helper()
	var mu sync.Mutex
	var body interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		if body == nil {
			var v interface{}
			if json.Unmarshal(raw, &v) != nil {
				v = string(raw)
			}
			body = v
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()
	call(srv.URL)
	mu.Lock()
	defer mu.Unlock()
	return body
}

// r91provbTypedCase is one Go-typed provider options literal and its
// map[string]interface{} / []interface{} / float64 JSON form. TS options are
// JSON values, so both must send the same request.
type r91provbTypedCase struct {
	name    string
	typed   interface{}
	generic interface{}
	run     func(base string, opts interface{})
}

func r91provbRunTypedCases(t *testing.T, cases []r91provbTypedCase) {
	t.Helper()
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			want := r91provbCapture(t, func(base string) { c.run(base, c.generic) })
			got := r91provbCapture(t, func(base string) { c.run(base, c.typed) })
			if want == nil {
				t.Fatal("generic form sent no request")
			}
			if !reflect.DeepEqual(got, want) {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(want)
				t.Fatalf("typed literal body differs\n got: %s\nwant: %s", g, w)
			}
		})
	}
}

func r91provbPrompt() types.Prompt {
	return types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}
}

func r91provbGenerate(m provider.LanguageModel, err error, opts *provider.GenerateOptions) {
	if err != nil {
		panic(err)
	}
	_, _ = m.DoGenerate(context.Background(), opts)
}

// PB-01 (provb2a-11, provb2b-13): Go-typed option literals reach the wire
// like their JSON form across the OpenAI-compatible helper, Moonshot,
// Alibaba, DeepSeek, Fireworks, Z.AI, Together and Open Responses readers.
func TestR91provbTypedOptionLiteralsCompat(t *testing.T) {
	budget := 2048
	enabled := true
	chat := func(mk func(base string) (provider.LanguageModel, error), key string) func(string, interface{}) {
		return func(base string, o interface{}) {
			m, err := mk(base)
			r91provbGenerate(m, err, &provider.GenerateOptions{Prompt: r91provbPrompt(), ProviderOptions: map[string]interface{}{key: o}})
		}
	}
	moon := func(base string) (provider.LanguageModel, error) {
		return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: base}).LanguageModel("kimi-k2.5")
	}
	ds := func(base string) (provider.LanguageModel, error) {
		return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: base}).LanguageModel("deepseek-chat")
	}
	fw := func(base string) (provider.LanguageModel, error) {
		return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: base}).LanguageModel("accounts/fireworks/models/kimi-k2p5")
	}
	z := func(base string) (provider.LanguageModel, error) {
		return zai.New(zai.Config{APIKey: "k", BaseURL: base}).LanguageModel("glm-4.6")
	}
	tg := func(base string) (provider.LanguageModel, error) {
		return together.New(together.Config{APIKey: "k", BaseURL: base}).LanguageModel("meta-llama/Llama-3-8b")
	}
	al := func(base string) (provider.LanguageModel, error) {
		return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: base}).LanguageModel("qwen3-max")
	}
	or := func(base string) (provider.LanguageModel, error) {
		return openresponses.New(openresponses.Config{BaseURL: base, Name: "acme"}).LanguageModel("m")
	}
	r91provbRunTypedCases(t, []r91provbTypedCase{
		{"moonshot thinking map[string]string", map[string]interface{}{"thinking": map[string]string{"type": "disabled"}},
			map[string]interface{}{"thinking": map[string]interface{}{"type": "disabled"}}, chat(moon, "moonshot")},
		{"moonshot promptCacheOptions", map[string]interface{}{"promptCacheOptions": map[string]string{"ttl": "1h"}},
			map[string]interface{}{"promptCacheOptions": map[string]interface{}{"ttl": "1h"}}, chat(moon, "moonshot")},
		{"moonshot prediction []map[string]string", map[string]interface{}{"prediction": map[string]interface{}{"type": "content", "content": []map[string]string{{"type": "text", "text": "x"}}}},
			map[string]interface{}{"prediction": map[string]interface{}{"type": "content", "content": []interface{}{map[string]interface{}{"type": "text", "text": "x"}}}}, chat(moon, "moonshot")},
		{"moonshotai namespace map[string]string", map[string]string{"promptCacheKey": "pk"},
			map[string]interface{}{"promptCacheKey": "pk"}, chat(moon, "moonshotai")},
		{"moonshot topLogprobs int64", map[string]interface{}{"logprobs": true, "topLogprobs": int64(3)},
			map[string]interface{}{"logprobs": true, "topLogprobs": float64(3)}, chat(moon, "moonshot")},
		{"deepseek namespace map[string]string", map[string]string{"reasoningEffort": "high"},
			map[string]interface{}{"reasoningEffort": "high"}, chat(ds, "deepseek")},
		{"deepseek thinking map[string]string", map[string]interface{}{"thinking": map[string]string{"type": "enabled"}},
			map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}}, chat(ds, "deepseek")},
		{"fireworks WithThinking", map[string]interface{}{"thinking": fireworks.WithThinkingBudget(budget)},
			map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": float64(budget)}}, chat(fw, "fireworks")},
		{"fireworks typed options struct", &fireworks.FireworksLanguageModelOptions{Thinking: fireworks.WithThinking(true), ReasoningHistory: "preserved"},
			map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}, "reasoningHistory": "preserved"}, chat(fw, "fireworks")},
		{"zai *ThinkingConfig", map[string]interface{}{"thinking": &zai.ThinkingConfig{Type: zai.ThinkingEnabled, ClearThinking: &enabled}},
			map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "clearThinking": true}}, chat(z, "zai")},
		{"togetherai namespace map[string]string", map[string]string{"user": "u1"},
			map[string]interface{}{"user": "u1"}, chat(tg, "togetherai")},
		{"alibaba map[string]bool", map[string]bool{"enableThinking": true},
			map[string]interface{}{"enableThinking": true}, chat(al, "alibaba")},
		{"open responses namespace map[string]string", map[string]string{"user": "u1"},
			map[string]interface{}{"user": "u1"}, chat(or, "acme")},
		{"open responses nested typed", map[string]interface{}{
			"allowedTools":      map[string][]string{"toolNames": {"a"}},
			"contextManagement": []map[string]interface{}{{"type": "compaction", "compactThreshold": int32(1000)}},
			"logprobs":          int32(3),
		}, map[string]interface{}{
			"allowedTools":      map[string]interface{}{"toolNames": []interface{}{"a"}},
			"contextManagement": []interface{}{map[string]interface{}{"type": "compaction", "compactThreshold": float64(1000)}},
			"logprobs":          float64(3),
		}, func(base string, o interface{}) {
			m, err := or(base)
			r91provbGenerate(m, err, &provider.GenerateOptions{Prompt: r91provbPrompt(), ProviderOptions: map[string]interface{}{"acme": o},
				Tools: []types.Tool{{Name: "a", Parameters: map[string]interface{}{"type": "object"}}}})
		}},
		{"moonshot message options map[string]string", map[string]string{"name": "n"},
			map[string]interface{}{"name": "n"}, func(base string, o interface{}) {
				m, err := moon(base)
				r91provbGenerate(m, err, &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
					{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}, ProviderOptions: map[string]interface{}{"moonshotai": o}},
				}}})
			}},
	})
}

// PB-01: image, video, embedding and rerank option readers.
func TestR91provbTypedOptionLiteralsMedia(t *testing.T) {
	r91provbRunTypedCases(t, []r91provbTypedCase{
		{"together image map[string]int", map[string]int{"steps": 4}, map[string]interface{}{"steps": float64(4)}, func(base string, o interface{}) {
			m, _ := together.New(together.Config{APIKey: "k", BaseURL: base}).ImageModel("black-forest-labs/FLUX.1-schnell")
			_, _ = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", ProviderOptions: map[string]interface{}{"togetherai": o}})
		}},
		{"fireworks image map[string]int", map[string]int{"seed": 4}, map[string]interface{}{"seed": float64(4)}, func(base string, o interface{}) {
			m, _ := fireworks.New(fireworks.Config{APIKey: "k", BaseURL: base}).ImageModel("accounts/fireworks/models/flux-1-dev-fp8")
			_, _ = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", ProviderOptions: map[string]interface{}{"fireworks": o}})
		}},
		{"together rerank map[string][]string", map[string][]string{"rankFields": {"title"}}, map[string]interface{}{"rankFields": []interface{}{"title"}}, func(base string, o interface{}) {
			m, _ := together.New(together.Config{APIKey: "k", BaseURL: base}).RerankingModel("Salesforce/Llama-Rank-v1")
			_, _ = m.DoRerank(context.Background(), &provider.RerankOptions{Query: "q", Documents: []map[string]interface{}{{"title": "t"}}, ProviderOptions: map[string]interface{}{"togetherai": o}})
		}},
		{"alibaba embedding map[string]string + int32", map[string]interface{}{"textType": "query", "dimension": int32(512)},
			map[string]interface{}{"textType": "query", "dimension": float64(512)}, func(base string, o interface{}) {
				m, _ := alibaba.New(alibaba.Config{APIKey: "k", EmbeddingBaseURL: base}).EmbeddingModel("text-embedding-v4")
				_, _ = m.DoEmbed(context.Background(), "x", &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"alibaba": o}})
			}},
		{"alibaba video typed media and map[string]string", map[string]interface{}{
			"negativePrompt": "blur",
			"media":          []map[string]string{{"type": "first_frame", "url": "https://example.com/a.png"}},
		}, map[string]interface{}{
			"negativePrompt": "blur",
			"media":          []interface{}{map[string]interface{}{"type": "first_frame", "url": "https://example.com/a.png"}},
		}, func(base string, o interface{}) {
			m, _ := alibaba.New(alibaba.Config{APIKey: "k", VideoBaseURL: base}).VideoModel("wan3-video")
			vm := m.(interface {
				DoStart(context.Context, *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error)
			})
			_, _ = vm.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
				Prompt: "p", ProviderOptions: map[string]interface{}{"alibaba": o}}})
		}},
	})
}
