package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
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
		var v interface{}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			// Multipart boundaries are random: record the fields and files.
			if err := r.ParseMultipartForm(1 << 20); err == nil {
				fields := map[string]interface{}{}
				for k, vs := range r.MultipartForm.Value {
					fields[k] = strings.Join(vs, ",")
				}
				for k, fs := range r.MultipartForm.File {
					fields["file:"+k] = fs[0].Filename
				}
				v = fields
			}
		} else {
			raw, _ := io.ReadAll(r.Body)
			if json.Unmarshal(raw, &v) != nil {
				v = string(raw)
			}
		}
		mu.Lock()
		if body == nil {
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

// PB-01 (provb1-11): the OpenAI and Azure readers (Responses, chat,
// completion, speech, transcription, embedding; part and tool options) read
// Go-typed literals like their JSON form.
func TestR91provbTypedOptionLiteralsOpenAI(t *testing.T) {
	oa := func(base string) *openai.Provider { return openai.New(openai.Config{APIKey: "k", BaseURL: base}) }
	gen := func(mk func(p *openai.Provider) (provider.LanguageModel, error), build func(o interface{}) *provider.GenerateOptions) func(string, interface{}) {
		return func(base string, o interface{}) {
			m, err := mk(oa(base))
			r91provbGenerate(m, err, build(o))
		}
	}
	ns := func(o interface{}) *provider.GenerateOptions {
		return &provider.GenerateOptions{Prompt: r91provbPrompt(), ProviderOptions: map[string]interface{}{"openai": o}}
	}
	responses := func(p *openai.Provider) (provider.LanguageModel, error) { return p.ResponsesModel("gpt-5") }
	chatM := func(p *openai.Provider) (provider.LanguageModel, error) { return p.ChatModel("gpt-4o") }
	completion := func(p *openai.Provider) (provider.LanguageModel, error) {
		return p.CompletionModel("gpt-3.5-turbo-instruct")
	}
	imagePart := func(o interface{}) *provider.GenerateOptions {
		return &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.ImageContent{Image: []byte{1, 2, 3}, MimeType: "image/png", ProviderOptions: map[string]interface{}{"openai": o}},
		}}}}}
	}
	toolOpts := func(o interface{}) *provider.GenerateOptions {
		return &provider.GenerateOptions{Prompt: r91provbPrompt(), Tools: []types.Tool{{Name: "f", Parameters: map[string]interface{}{"type": "object"}, ProviderOptions: o}}}
	}
	r91provbRunTypedCases(t, []r91provbTypedCase{
		{"responses namespace map[string]string", map[string]string{"user": "u1"}, map[string]interface{}{"user": "u1"}, gen(responses, ns)},
		{"responses promptCacheOptions map[string]string", map[string]interface{}{"promptCacheOptions": map[string]string{"mode": "explicit"}},
			map[string]interface{}{"promptCacheOptions": map[string]interface{}{"mode": "explicit"}}, gen(responses, ns)},
		{"responses allowedTools map[string][]string + int32", map[string]interface{}{"allowedTools": map[string][]string{"toolNames": {"f"}}, "maxToolCalls": int32(3), "logprobs": int64(2)},
			map[string]interface{}{"allowedTools": map[string]interface{}{"toolNames": []interface{}{"f"}}, "maxToolCalls": float64(3), "logprobs": float64(2)}, func(base string, o interface{}) {
				m, err := responses(oa(base))
				opts := ns(o)
				opts.Tools = []types.Tool{{Name: "f", Parameters: map[string]interface{}{"type": "object"}}}
				r91provbGenerate(m, err, opts)
			}},
		{"responses image part map[string]string", map[string]string{"imageDetail": "high"}, map[string]interface{}{"imageDetail": "high"}, gen(responses, imagePart)},
		{"responses tool options typed map", map[string]map[string]interface{}{"openai": {"deferLoading": true}},
			map[string]interface{}{"openai": map[string]interface{}{"deferLoading": true}}, gen(responses, toolOpts)},
		{"chat namespace map[string]string", map[string]string{"user": "u1"}, map[string]interface{}{"user": "u1"}, gen(chatM, ns)},
		{"chat logprobs int32 + logitBias map[string]int", map[string]interface{}{"logprobs": int32(2), "logitBias": map[string]int{"50256": -100}},
			map[string]interface{}{"logprobs": float64(2), "logitBias": map[string]interface{}{"50256": float64(-100)}}, gen(chatM, ns)},
		{"chat image part map[string]string", map[string]string{"imageDetail": "low"}, map[string]interface{}{"imageDetail": "low"}, gen(chatM, imagePart)},
		{"completion namespace map[string]string", map[string]string{"user": "u1"}, map[string]interface{}{"user": "u1"}, gen(completion, ns)},
		{"speech speed int64", map[string]interface{}{"speed": int64(2)}, map[string]interface{}{"speed": float64(2)}, func(base string, o interface{}) {
			m, _ := oa(base).SpeechModel("tts-1")
			_, _ = m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", ProviderOptions: map[string]interface{}{"openai": o}})
		}},
		{"transcription namespace map[string]string", map[string]string{"language": "en", "prompt": "p"}, map[string]interface{}{"language": "en", "prompt": "p"}, func(base string, o interface{}) {
			m, _ := oa(base).TranscriptionModel("whisper-1")
			_, _ = m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("RIFF"), MimeType: "audio/wav", ProviderOptions: map[string]interface{}{"openai": o}})
		}},
		{"embedding map[string]int", map[string]int{"dimensions": 64}, map[string]interface{}{"dimensions": float64(64)}, func(base string, o interface{}) {
			m, _ := oa(base).EmbeddingModel("text-embedding-3-small")
			_, _ = m.DoEmbed(context.Background(), "x", &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"openai": o}})
		}},
		{"azure transcription namespace map[string]string", map[string]string{"language": "en"}, map[string]interface{}{"language": "en"}, func(base string, o interface{}) {
			m, _ := r91provbAzure(base).TranscriptionModel("whisper-1")
			_, _ = m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("RIFF"), MimeType: "audio/wav", ProviderOptions: map[string]interface{}{"azure": o}})
		}},
		{"azure speech namespace map[string]interface{} int64", map[string]interface{}{"speed": int64(2)}, map[string]interface{}{"speed": float64(2)}, func(base string, o interface{}) {
			m, _ := r91provbAzure(base).SpeechModel("tts-1")
			_, _ = m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", ProviderOptions: map[string]interface{}{"azure": o}})
		}},
		{"azure image namespace map[string]string", map[string]string{"quality": "high"}, map[string]interface{}{"quality": "high"}, func(base string, o interface{}) {
			m, _ := r91provbAzure(base).ImageModel("gpt-image-1")
			_, _ = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", ProviderOptions: map[string]interface{}{"azure": o}})
		}},
		{"azure embedding map[string]int", map[string]int{"dimensions": 64}, map[string]interface{}{"dimensions": float64(64)}, func(base string, o interface{}) {
			m, _ := r91provbAzure(base).EmbeddingModel("text-embedding-3-small")
			_, _ = m.DoEmbed(context.Background(), "x", &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"openai": o}})
		}},
	})
}

func r91provbAzure(base string) *azure.Provider {
	p, err := azure.New(azure.Config{APIKey: "k", BaseURL: base})
	if err != nil {
		panic(err)
	}
	return p
}

// PB-02 (provg2-01 provb sites): a System-only prompt panicked on the
// unchecked body["messages"] assertion; TS sends [{role: system}]
// (convert-to-openai-compatible-chat-messages.ts:227).
func TestR91provbSystemOnlyPrompt(t *testing.T) {
	models := map[string]func(base string) (provider.LanguageModel, error){
		"together": func(base string) (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		},
		"fireworks": func(base string) (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		},
		"ollama": func(base string) (provider.LanguageModel, error) {
			return ollama.New(ollama.Config{BaseURL: base}).LanguageModel("m")
		},
	}
	for name, mk := range models {
		mk := mk
		t.Run(name, func(t *testing.T) {
			body := r91provbCapture(t, func(base string) {
				m, err := mk(base)
				r91provbGenerate(m, err, &provider.GenerateOptions{Prompt: types.Prompt{System: "be brief"}})
			})
			msgs, _ := body.(map[string]interface{})["messages"].([]interface{})
			want := []interface{}{map[string]interface{}{"role": "system", "content": "be brief"}}
			if !reflect.DeepEqual(msgs, want) {
				t.Fatalf("messages = %#v, want %#v", msgs, want)
			}
		})
	}
}

// r91provbServe answers every request with a 200 JSON body.
func r91provbServe(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// PB-04 (provb2a-01 = provb2b-03): Moonshot and DeepSeek doGenerate content
// is reasoning, tool calls, then text (moonshotai-chat-language-model.ts:503-525,
// deepseek-chat-language-model.ts:447-472).
func TestR91provbToolCallBeforeTextContent(t *testing.T) {
	resp := `{"id":"r","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"done","reasoning_content":"think","tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{\"a\":1}"}}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	models := map[string]func(base string) (provider.LanguageModel, error){
		"moonshot": func(base string) (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: base}).LanguageModel("kimi-k2.5")
		},
		"deepseek": func(base string) (provider.LanguageModel, error) {
			return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: base}).LanguageModel("deepseek-chat")
		},
	}
	for name, mk := range models {
		mk := mk
		t.Run(name, func(t *testing.T) {
			srv := r91provbServe(t, resp)
			m, err := mk(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			zero := 0
			out, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: m, Prompt: "hi", MaxRetries: &zero})
			if err != nil {
				t.Fatal(err)
			}
			var kinds []string
			for _, p := range out.Steps[0].Content {
				kinds = append(kinds, reflect.TypeOf(p).Name())
			}
			// The unknown tool then adds a tool error after the model content.
			want := []string{"ReasoningContent", "ToolCallContent", "TextContent", "ToolErrorContent"}
			if !reflect.DeepEqual(kinds, want) {
				t.Fatalf("content = %v, want %v", kinds, want)
			}
		})
	}
}

// r91provbChatModels is every provb language model entry point.
func r91provbChatModels() map[string]func(base string) (provider.LanguageModel, error) {
	return map[string]func(base string) (provider.LanguageModel, error){
		"together": func(b string) (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
		"fireworks": func(b string) (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
		"ollama": func(b string) (provider.LanguageModel, error) {
			return ollama.New(ollama.Config{BaseURL: b}).LanguageModel("m")
		},
		"gmicloud": func(b string) (provider.LanguageModel, error) {
			return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
		"zai": func(b string) (provider.LanguageModel, error) {
			return zai.New(zai.Config{APIKey: "k", BaseURL: b}).LanguageModel("glm-4.6")
		},
		"moonshot": func(b string) (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: b}).LanguageModel("kimi-k2.5")
		},
		"deepseek": func(b string) (provider.LanguageModel, error) {
			return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: b}).LanguageModel("deepseek-chat")
		},
		"alibaba": func(b string) (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: b}).LanguageModel("qwen3-max")
		},
		"openai chat": func(b string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: b}).ChatModel("gpt-4o")
		},
		"openai responses": func(b string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: b}).ResponsesModel("gpt-5")
		},
		"openai completion": func(b string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: b}).CompletionModel("gpt-3.5-turbo-instruct")
		},
		"azure chat":      func(b string) (provider.LanguageModel, error) { return r91provbAzure(b).ChatModel("gpt-4o") },
		"azure responses": func(b string) (provider.LanguageModel, error) { return r91provbAzure(b).ResponsesModel("gpt-5") },
		"xai": func(b string) (provider.LanguageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: b}).LanguageModel("grok-4")
		},
		"open responses": func(b string) (provider.LanguageModel, error) {
			return openresponses.New(openresponses.Config{BaseURL: b, Name: "acme"}).LanguageModel("m")
		},
		"huggingface": func(b string) (provider.LanguageModel, error) {
			return huggingface.New(huggingface.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
	}
}

// PB-05 (provb2b-01) and the risk-note sweep: a Prompt.Text prompt sends
// what the one-user-message form sends (Open Responses sent input: null,
// Hugging Face and xAI Responses input: []), and no model panics on a
// System-only prompt.
func TestR91provbPromptForms(t *testing.T) {
	for name, mk := range r91provbChatModels() {
		mk := mk
		t.Run(name, func(t *testing.T) {
			want := r91provbCapture(t, func(base string) {
				m, err := mk(base)
				r91provbGenerate(m, err, &provider.GenerateOptions{Prompt: r91provbPrompt()})
			})
			got := r91provbCapture(t, func(base string) {
				m, err := mk(base)
				r91provbGenerate(m, err, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			})
			if want == nil || !reflect.DeepEqual(got, want) {
				g, _ := json.Marshal(got)
				w, _ := json.Marshal(want)
				t.Fatalf("Prompt.Text body differs\n got: %s\nwant: %s", g, w)
			}
			if r91provbCapture(t, func(base string) {
				m, err := mk(base)
				r91provbGenerate(m, err, &provider.GenerateOptions{Prompt: types.Prompt{System: "s"}})
			}) == nil {
				t.Fatal("System-only prompt sent no request")
			}
		})
	}
}

// PB-06 (provb2b-04): Fireworks async (flux-kontext) submit and poll HTTP
// failures are ProviderErrors with the status text (TS postJsonToApi with
// createStatusCodeErrorResponseHandler, fireworks-image-model.ts:240), not
// plain wrapped errors.
func TestR91provbFireworksAsyncStatusErrors(t *testing.T) {
	for _, failPoll := range []bool{false, true} {
		failPoll := failPoll
		t.Run(map[bool]string{false: "submit", true: "poll"}[failPoll], func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/get_result") || !failPoll {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"error":"busy"}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"request_id":"req-1"}`))
			}))
			defer srv.Close()
			m, _ := fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL, ImagePollIntervalMs: 1}).ImageModel("accounts/fireworks/models/flux-kontext-pro")
			_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.StatusCode != http.StatusServiceUnavailable || pe.Message != "Service Unavailable" {
				t.Fatalf("err = %#v, want a 503 ProviderError with the status text", err)
			}
		})
	}
}

// PB-29 (provb2b-09): Fireworks transformRequestBody copies values as given
// (fireworks-provider.ts:177).
func TestR91provbFireworksTransformVerbatim(t *testing.T) {
	body := r91provbCapture(t, func(base string) {
		m, err := fireworks.New(fireworks.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		r91provbGenerate(m, err, &provider.GenerateOptions{Prompt: r91provbPrompt(), ProviderOptions: map[string]interface{}{"fireworks": map[string]interface{}{
			"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": 1500.5}, "promptCacheKey": nil,
		}}})
	}).(map[string]interface{})
	th, _ := body["thinking"].(map[string]interface{})
	if th["budget_tokens"] != 1500.5 {
		t.Fatalf("thinking = %v", body["thinking"])
	}
	if v, present := body["prompt_cache_key"]; !present || v != nil {
		t.Fatalf("prompt_cache_key = %v, present %v", v, present)
	}
}

// PB-30 (provb2b-10): image size is split on "x" like JS: Together parses
// each part with parseInt, Fireworks sends the parts as given.
func TestR91provbImageSizeSplit(t *testing.T) {
	tb := r91provbCapture(t, func(base string) {
		m, _ := together.New(together.Config{APIKey: "k", BaseURL: base}).ImageModel("black-forest-labs/FLUX.1-schnell")
		_, _ = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", Size: "1024.5x768"})
	}).(map[string]interface{})
	if tb["width"] != float64(1024) || tb["height"] != float64(768) {
		t.Fatalf("together = %v x %v", tb["width"], tb["height"])
	}
	fb := r91provbCapture(t, func(base string) {
		m, _ := fireworks.New(fireworks.Config{APIKey: "k", BaseURL: base}).ImageModel("accounts/fireworks/models/flux-1-dev-fp8")
		_, _ = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", Size: "1024"})
	}).(map[string]interface{})
	if _, hasHeight := fb["height"]; fb["width"] != "1024" || hasHeight {
		t.Fatalf("fireworks = %v", fb)
	}
}

// r91provbRawBody captures the first request body as sent.
func r91provbRawBody(t *testing.T, call func(base string)) string {
	t.Helper()
	var mu sync.Mutex
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		if body == "" {
			body = string(raw)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	call(srv.URL)
	mu.Lock()
	defer mu.Unlock()
	return body
}

// PB-27 (provb2b-05): the GMI Cloud streaming body is sent as built, with <
// and & literal (TS JSON.stringify), not re-marshaled by the transport.
func TestR91provbGmicloudStreamBodyVerbatim(t *testing.T) {
	body := r91provbRawBody(t, func(base string) {
		m, _ := gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		_, _ = m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "a<b&c"}}}}}})
	})
	if !strings.Contains(body, "a<b&c") || !strings.Contains(body, `"include_usage":true`) {
		t.Fatalf("body = %s", body)
	}
}

// PB-32 (provb2b-12): Hugging Face text parts keep an empty text
// (convert-to-huggingface-responses-messages.ts:68).
func TestR91provbHuggingFaceEmptyText(t *testing.T) {
	body := r91provbRawBody(t, func(base string) {
		m, _ := huggingface.New(huggingface.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: ""}}},
		}}})
	})
	if !strings.Contains(body, `{"type":"output_text","text":""}`) {
		t.Fatalf("body = %s", body)
	}
}

// TS f9d43bdaa7: the DeepSeek Responses model tests
// (deepseek-responses-language-model.test.ts, recorded at 2ebc78b654) replay
// through deepseek.ResponsesModel. The recording is kept apart from
// deepseek.jsonl.gz so the chat suite stays as it is; the integrator can
// fold it into the registry (cr-r91-provb-to-integrator).
func TestR91provbDeepSeekResponsesWire(t *testing.T) {
	suite := wireSuites["deepseek"]
	suite.Language = func(p provider.Provider, cls, id string) (provider.LanguageModel, error) {
		if cls == "DeepSeekResponsesLanguageModel" {
			return p.(*deepseek.Provider).ResponsesModel(id)
		}
		return p.LanguageModel(id)
	}
	tsfixture.RunWire(t, "testdata/tswire/deepseek-responses.jsonl.gz", suite)
}
