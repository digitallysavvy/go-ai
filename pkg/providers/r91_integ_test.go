package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/assemblyai"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/baseten"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/bfl"
	"github.com/digitallysavvy/go-ai/pkg/providers/cartesia"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepgram"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
	"github.com/digitallysavvy/go-ai/pkg/providers/fal"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/fishaudio"
	"github.com/digitallysavvy/go-ai/pkg/providers/gladia"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/hume"
	"github.com/digitallysavvy/go-ai/pkg/providers/lmnt"
	"github.com/digitallysavvy/go-ai/pkg/providers/luma"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/prodia"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
	"github.com/digitallysavvy/go-ai/pkg/providers/replicate"
	"github.com/digitallysavvy/go-ai/pkg/providers/revai"
	"github.com/digitallysavvy/go-ai/pkg/providers/stability"
	"github.com/digitallysavvy/go-ai/pkg/providers/tencent"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/topaz"
	"github.com/digitallysavvy/go-ai/pkg/providers/typesafeai"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// PB-22 class sweep (cr-r91-provb-to-integrator 2, TS handle-fetch-error.ts):
// an aborted call (here: a cancelled context, no HTTP response) returns the
// abort error itself, never a status-0 *ProviderError, for every model type
// of these providers. The CR listed 14 sites; this probe found the class in
// every error handler that checks FetchRejectionCause (80 sites), which now
// also checks internalhttp.NoResponseError. A failed connection keeps its
// "Cannot connect to API" error (TS APICallError).
func TestR91integTransportErrorsUnchangedSweep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	azureP, _ := azure.New(azure.Config{APIKey: "k", BaseURL: srv.URL})
	providers := map[string]interface{}{
		"alibaba":       alibaba.New(alibaba.Config{APIKey: "k", BaseURL: srv.URL}),
		"anthropic":     anthropic.New(anthropic.Config{APIKey: "k", BaseURL: srv.URL}),
		"assemblyai":    assemblyai.New(assemblyai.Config{APIKey: "k", BaseURL: srv.URL}),
		"azure":         azureP,
		"bfl":           bfl.New(bfl.Config{APIKey: "k", BaseURL: srv.URL}),
		"cartesia":      cartesia.New(cartesia.Config{APIKey: "k", BaseURL: srv.URL}),
		"deepgram":      deepgram.New(deepgram.Config{APIKey: "k", BaseURL: srv.URL}),
		"deepseek":      deepseek.New(deepseek.Config{APIKey: "k", BaseURL: srv.URL}),
		"elevenlabs":    elevenlabs.New(elevenlabs.Config{APIKey: "k", BaseURL: srv.URL}),
		"fal":           fal.New(fal.Config{APIKey: "k", BaseURL: srv.URL}),
		"fishaudio":     fishaudio.New(fishaudio.Config{APIKey: "k", BaseURL: srv.URL}),
		"gladia":        gladia.New(gladia.Config{APIKey: "k", BaseURL: srv.URL}),
		"gmicloud":      gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: srv.URL}),
		"huggingface":   huggingface.New(huggingface.Config{APIKey: "k", BaseURL: srv.URL}),
		"hume":          hume.New(hume.Config{APIKey: "k", BaseURL: srv.URL}),
		"lmnt":          lmnt.New(lmnt.Config{APIKey: "k", BaseURL: srv.URL}),
		"luma":          luma.New(luma.Config{APIKey: "k", BaseURL: srv.URL}),
		"moonshot":      moonshot.New(moonshot.Config{APIKey: "k", BaseURL: srv.URL}),
		"openai":        openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}),
		"openresponses": openresponses.New(openresponses.Config{Name: "x", APIKey: "k", BaseURL: srv.URL}),
		"prodia":        prodia.New(prodia.Config{APIKey: "k", BaseURL: srv.URL}),
		"replicate":     replicate.New(replicate.Config{APIKey: "k", BaseURL: srv.URL}),
		"revai":         revai.New(revai.Config{APIKey: "k", BaseURL: srv.URL}),
		"stability":     stability.New(stability.Config{APIKey: "k", BaseURL: srv.URL}),
		"tencent":       tencent.New(tencent.Config{APIKey: "k", BaseURL: srv.URL}),
		"topaz":         topaz.New(topaz.Config{APIKey: "k", BaseURL: srv.URL}),
		"typesafeai":    typesafeai.New(typesafeai.Config{APIKey: "k", BaseURL: srv.URL}),
		"xai":           xai.New(xai.Config{APIKey: "k", BaseURL: srv.URL}),
		"zai":           zai.New(zai.Config{APIKey: "k", BaseURL: srv.URL}),
		"baseten":       baseten.New(baseten.Config{APIKey: "k", BaseURL: srv.URL}),
		"bedrock":       bedrock.New(bedrock.Config{APIKey: "k", Region: "us-east-1", BaseURL: srv.URL}),
		"cerebras":      cerebras.New(cerebras.Config{APIKey: "k", BaseURL: srv.URL}),
		"cohere":        cohere.New(cohere.Config{APIKey: "k", BaseURL: srv.URL}),
		"deepinfra":     deepinfra.New(deepinfra.Config{APIKey: "k", BaseURL: srv.URL}),
		"fireworks":     fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}),
		"google":        google.New(google.Config{APIKey: "k", BaseURL: srv.URL}),
		"groq":          groq.New(groq.Config{APIKey: "k", BaseURL: srv.URL}),
		"minimax":       minimax.New(minimax.Config{APIKey: "k", BaseURL: srv.URL, ImageBaseURL: srv.URL}),
		"mistral":       mistral.New(mistral.Config{APIKey: "k", BaseURL: srv.URL}),
		"perplexity":    perplexity.New(perplexity.Config{APIKey: "k", BaseURL: srv.URL}),
		"quiverai":      quiverai.New(quiverai.Config{APIKey: "k", BaseURL: srv.URL}),
		"together":      together.New(together.Config{APIKey: "k", BaseURL: srv.URL}),
		"voyage":        voyage.New(voyage.Config{APIKey: "k", BaseURL: srv.URL}),
	}
	prompt := types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}
	var failures []string
	check := func(name string, err error) {
		var nsm *providererrors.NoSuchModelError
		if errors.As(err, &nsm) {
			return
		}
		var pe *providererrors.ProviderError
		if err == nil || !errors.Is(err, context.Canceled) || errors.As(err, &pe) {
			failures = append(failures, name+": "+errString(err))
		}
	}
	for name, p := range providers {
		if f, ok := p.(interface {
			LanguageModel(string) (provider.LanguageModel, error)
		}); ok {
			if m, err := f.LanguageModel("m"); err == nil {
				_, err = m.DoGenerate(ctx, &provider.GenerateOptions{Prompt: prompt})
				check(name+" language generate", err)
				_, err = m.DoStream(ctx, &provider.GenerateOptions{Prompt: prompt})
				check(name+" language stream", err)
			}
		}
		if f, ok := p.(interface {
			EmbeddingModel(string) (provider.EmbeddingModel, error)
		}); ok {
			if m, err := f.EmbeddingModel("m"); err == nil {
				_, err = m.DoEmbed(ctx, "x", &provider.EmbedModelOptions{})
				check(name+" embedding", err)
			}
		}
		if f, ok := p.(interface {
			ImageModel(string) (provider.ImageModel, error)
		}); ok {
			id := "m"
			if name == "google" {
				id = "gemini-2.5-flash-image" // Imagen IDs fail before any request
			}
			if m, err := f.ImageModel(id); err == nil {
				opts := &provider.ImageGenerateOptions{Prompt: "p"}
				if name == "topaz" { // Topaz enhances an input image
					opts.Files = []provider.ImageFile{{Type: "file", Data: []byte{0x89, 'P', 'N', 'G'}, MediaType: "image/png"}}
				}
				_, err = m.DoGenerate(ctx, opts)
				check(name+" image", err)
			}
		}
		if f, ok := p.(interface {
			SpeechModel(string) (provider.SpeechModel, error)
		}); ok {
			if m, err := f.SpeechModel("m"); err == nil {
				_, err = m.DoGenerate(ctx, &provider.SpeechGenerateOptions{Text: "hi", Voice: "v"})
				check(name+" speech", err)
			}
		}
		if f, ok := p.(interface {
			TranscriptionModel(string) (provider.TranscriptionModel, error)
		}); ok {
			// Cartesia transcribes only through its streaming model.
			if m, err := f.TranscriptionModel("m"); err == nil && name != "cartesia" {
				_, err = m.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte("RIFF0000WAVE"), MimeType: "audio/wav"})
				check(name+" transcription", err)
			}
		}
		if f, ok := p.(interface {
			RerankingModel(string) (provider.RerankingModel, error)
		}); ok {
			if m, err := f.RerankingModel("m"); err == nil {
				_, err = m.DoRerank(ctx, &provider.RerankOptions{Documents: []string{"a"}, Query: "q"})
				check(name+" reranking", err)
			}
		}
	}
	rt := mistral.New(mistral.Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalRealtime()
	_, err := rt.CreateSession(ctx, mistral.RealtimeCreateSessionOptions{Model: "m"})
	check("mistral realtime create", err)
	check("mistral realtime close", rt.CloseSession(ctx, "s1"))
	sort.Strings(failures)
	for _, f := range failures {
		t.Error(f)
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// r91integCapture is a server that records the last request body and answers
// with a 400 error so every call ends after its first request.
type r91integCapture struct {
	mu   sync.Mutex
	body string
	srv  *httptest.Server
}

func newR91integCapture(t *testing.T) *r91integCapture {
	c := &r91integCapture{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.body = string(b)
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop","type":"invalid_request_error"}}`))
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *r91integCapture) take() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.body
	c.body = ""
	return b
}

// r91integJSONForm is the JSON form of a Go value (what TS sees as the
// provider options object).
func r91integJSONForm(t *testing.T, v interface{}) interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Repo-wide typed-option probe (coordinator typed-options ruling, round 91):
// for each provider with provider-option readers, a Go-typed literal
// (map[string]string, []map[string]interface{}, a typed option struct,
// int64 / float32 numbers) sends the same request body as its JSON form,
// and that body carries the option (TS options are plain JSON values).
func TestR91integTypedProviderOptionLiteralsRepoWide(t *testing.T) {
	c := newR91integCapture(t)
	base := c.srv.URL
	prompt := types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}
	type lm func() (provider.LanguageModel, error)
	compat := func(name string, mk lm) []struct {
		name, ns string
		mk       lm
		value    interface{}
		want     string
	} {
		return []struct {
			name, ns string
			mk       lm
			value    interface{}
			want     string
		}{{name + " map[string]string", name, mk, map[string]string{"user": "u1"}, `"user":"u1"`}}
	}
	type tc = struct {
		name, ns string
		mk       lm
		value    interface{}
		want     string
	}
	var cases []tc
	add := func(cs ...tc) { cases = append(cases, cs...) }
	openaiResp := func() (provider.LanguageModel, error) {
		return openai.New(openai.Config{APIKey: "k", BaseURL: base}).ResponsesModel("gpt-5")
	}
	openaiChat := func() (provider.LanguageModel, error) {
		return openai.New(openai.Config{APIKey: "k", BaseURL: base}).ChatModel("gpt-4o")
	}
	add(
		tc{"openai responses map[string]string", "openai", openaiResp, map[string]string{"user": "u1"}, `"user":"u1"`},
		tc{"openai responses int64", "openai", openaiResp, map[string]interface{}{"maxToolCalls": int64(3)}, `"max_tool_calls":3`},
		tc{"openai responses float32", "openai", openaiResp, map[string]interface{}{"logprobs": float32(2)}, `"top_logprobs":2`},
		tc{"openai chat map[string]string", "openai", openaiChat, map[string]string{"user": "u1"}, `"user":"u1"`},
		tc{"openai chat int64", "openai", openaiChat, map[string]interface{}{"maxCompletionTokens": int64(77)}, `"max_completion_tokens":77`},
	)
	anth := func() (provider.LanguageModel, error) {
		return anthropic.New(anthropic.Config{APIKey: "k", BaseURL: base}).LanguageModel("claude-sonnet-4-5")
	}
	add(
		tc{"anthropic map[string]string", "anthropic", anth, map[string]string{"serviceTier": "auto"}, `"service_tier":"auto"`},
		tc{"anthropic typed struct", "anthropic", anth, &anthropic.ModelOptions{ServiceTier: "auto"}, `"service_tier":"auto"`},
		tc{"anthropic int64", "anthropic", anth, map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": int64(2048)}}, `"budget_tokens":2048`},
		tc{"anthropic []map", "anthropic", anth, map[string]interface{}{"mcpServers": []map[string]interface{}{{"type": "url", "name": "x", "url": "https://mcp.example.com"}}}, `"mcp_servers":[{`},
	)
	goog := func() (provider.LanguageModel, error) {
		return google.New(google.Config{APIKey: "k", BaseURL: base}).LanguageModel("gemini-2.5-flash")
	}
	add(
		tc{"google []map", "google", goog, map[string]interface{}{"safetySettings": []map[string]interface{}{{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "BLOCK_NONE"}}}, `"safetySettings":[{"category":"HARM_CATEGORY_HATE_SPEECH"`},
		tc{"google []map[string]string", "google", goog, map[string]interface{}{"safetySettings": []map[string]string{{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "BLOCK_NONE"}}}, `"threshold":"BLOCK_NONE"`},
		tc{"google int64", "google", goog, map[string]interface{}{"thinkingConfig": map[string]interface{}{"thinkingBudget": int64(1024)}}, `"thinkingBudget":1024`},
		tc{"google map[string]string", "google", goog, map[string]string{"cachedContent": "cachedContents/c1"}, `"cachedContent":"cachedContents/c1"`},
	)
	mist := func() (provider.LanguageModel, error) {
		return mistral.New(mistral.Config{APIKey: "k", BaseURL: base}).LanguageModel("mistral-large-latest")
	}
	add(
		tc{"mistral int64", "mistral", mist, map[string]interface{}{"documentImageLimit": int64(4)}, `"document_image_limit":4`},
		tc{"mistral float32", "mistral", mist, map[string]interface{}{"documentPageLimit": float32(6)}, `"document_page_limit":6`},
	)
	coh := func() (provider.LanguageModel, error) {
		return cohere.New(cohere.Config{APIKey: "k", BaseURL: base}).LanguageModel("command-a-reasoning")
	}
	add(tc{"cohere int64", "cohere", coh, map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "tokenBudget": int64(100)}}, `"token_budget":100`})
	bed := func() (provider.LanguageModel, error) {
		return bedrock.New(bedrock.Config{APIKey: "k", Region: "us-east-1", BaseURL: base}).LanguageModel("amazon.nova-pro-v1:0")
	}
	add(tc{"bedrock int64", "bedrock", bed, map[string]interface{}{"additionalModelRequestFields": map[string]interface{}{"top_k": int64(5)}}, `"top_k":5`})
	add(tc{"open responses typed struct", "acme", func() (provider.LanguageModel, error) {
		return openresponses.New(openresponses.Config{Name: "acme", APIKey: "k", BaseURL: base}).LanguageModel("m")
	}, &openresponses.OpenResponsesProviderOptions{ReasoningEffort: "high"}, `"effort":"high"`})
	add(
		tc{"deepseek map[string]string", "deepseek", func() (provider.LanguageModel, error) {
			return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}, map[string]interface{}{"thinking": map[string]string{"type": "disabled"}}, `"thinking":{"type":"disabled"}`},
		tc{"moonshot int64", "moonshotai", func() (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}, map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": int64(2048)}}, `"thinking":{"type":"enabled"}`}, // budgetTokens is validated, then omitted (TS)
	)
	for _, p := range []struct {
		ns string
		mk lm
	}{
		{"groq", func() (provider.LanguageModel, error) {
			return groq.New(groq.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
		{"xai", func() (provider.LanguageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
		{"togetherai", func() (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
		{"fireworks", func() (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
		{"zai", func() (provider.LanguageModel, error) {
			return zai.New(zai.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
		{"gmicloud", func() (provider.LanguageModel, error) {
			return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
		{"alibaba", func() (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
		{"ollama", func() (provider.LanguageModel, error) {
			return ollama.New(ollama.Config{BaseURL: base}).LanguageModel("m")
		}},
		{"cerebras", func() (provider.LanguageModel, error) {
			return cerebras.New(cerebras.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
		{"deepinfra", func() (provider.LanguageModel, error) {
			return deepinfra.New(deepinfra.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
		{"baseten", func() (provider.LanguageModel, error) {
			return baseten.New(baseten.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
		{"tencent", func() (provider.LanguageModel, error) {
			return tencent.New(tencent.Config{APIKey: "k", BaseURL: base}).LanguageModel("m")
		}},
	} {
		add(compat(p.ns, p.mk)...)
	}

	send := func(mk lm, ns string, v interface{}) string {
		m, err := mk()
		if err != nil {
			t.Fatalf("model: %v", err)
		}
		c.take()
		_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: prompt, ProviderOptions: map[string]interface{}{ns: v}})
		return c.take()
	}
	var failures []string
	for _, tcase := range cases {
		typed := send(tcase.mk, tcase.ns, tcase.value)
		plain := send(tcase.mk, tcase.ns, r91integJSONForm(t, tcase.value))
		switch {
		case !strings.Contains(plain, tcase.want):
			failures = append(failures, fmt.Sprintf("%s: JSON form body lacks %s: %s", tcase.name, tcase.want, plain))
		case typed != plain:
			failures = append(failures, fmt.Sprintf("%s: typed body differs\n typed: %s\n plain: %s", tcase.name, typed, plain))
		}
	}
	for _, f := range failures {
		t.Error(f)
	}
}

// The typed-option probe over embedding, image, speech, transcription and
// reranking models (same check as the language models above).
func TestR91integTypedProviderOptionLiteralsMediaRepoWide(t *testing.T) {
	c := newR91integCapture(t)
	base := c.srv.URL
	type tc struct {
		name, ns string
		call     func(opts map[string]interface{})
		value    interface{}
		want     string
	}
	embed := func(mk func() (provider.EmbeddingModel, error)) func(map[string]interface{}) {
		return func(o map[string]interface{}) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			_, _ = m.DoEmbed(context.Background(), "x", &provider.EmbedModelOptions{ProviderOptions: o})
		}
	}
	image := func(mk func() (provider.ImageModel, error)) func(map[string]interface{}) {
		return func(o map[string]interface{}) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			_, _ = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p", ProviderOptions: o})
		}
	}
	speech := func(mk func() (provider.SpeechModel, error)) func(map[string]interface{}) {
		return func(o map[string]interface{}) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			_, _ = m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", Voice: "v", ProviderOptions: o})
		}
	}
	rerank := func(mk func() (provider.RerankingModel, error)) func(map[string]interface{}) {
		return func(o map[string]interface{}) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			_, _ = m.DoRerank(context.Background(), &provider.RerankOptions{Documents: []string{"a", "b"}, Query: "q", ProviderOptions: o})
		}
	}
	cases := []tc{
		{"openai embedding int64", "openai", embed(func() (provider.EmbeddingModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: base}).EmbeddingModel("text-embedding-3-small")
		}), map[string]interface{}{"dimensions": int64(64)}, `"dimensions":64`},
		{"google embedding int64", "google", embed(func() (provider.EmbeddingModel, error) {
			return google.New(google.Config{APIKey: "k", BaseURL: base}).EmbeddingModel("gemini-embedding-001")
		}), map[string]interface{}{"outputDimensionality": int64(64)}, `"outputDimensionality":64`},
		{"cohere embedding map[string]string", "cohere", embed(func() (provider.EmbeddingModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: base}).EmbeddingModel("embed-v4.0")
		}), map[string]string{"inputType": "search_query"}, `"input_type":"search_query"`},
		{"voyage embedding map[string]string", "voyage", embed(func() (provider.EmbeddingModel, error) {
			return voyage.New(voyage.Config{APIKey: "k", BaseURL: base}).EmbeddingModel("voyage-3.5")
		}), map[string]string{"inputType": "query"}, `"input_type":"query"`},
		{"mistral embedding int64", "mistral", embed(func() (provider.EmbeddingModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: base}).EmbeddingModel("codestral-embed")
		}), map[string]interface{}{"outputDimension": int64(64)}, `"output_dimension":64`},
		{"openai image map[string]string", "openai", image(func() (provider.ImageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: base}).ImageModel("gpt-image-1")
		}), map[string]string{"quality": "high"}, `"quality":"high"`},
		{"together image int64", "togetherai", image(func() (provider.ImageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: base}).ImageModel("black-forest-labs/FLUX.1-schnell")
		}), map[string]interface{}{"steps": int64(4)}, `"steps":4`},
		{"elevenlabs speech map[string]float64", "elevenlabs", speech(func() (provider.SpeechModel, error) {
			return elevenlabs.New(elevenlabs.Config{APIKey: "k", BaseURL: base}).SpeechModel("eleven_multilingual_v2")
		}), map[string]interface{}{"voiceSettings": map[string]float64{"stability": 0.5}}, `"stability":0.5`},
		{"cohere rerank int64", "cohere", rerank(func() (provider.RerankingModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: base}).RerankingModel("rerank-v3.5")
		}), map[string]interface{}{"maxTokensPerDoc": int64(100)}, `"max_tokens_per_doc":100`},
		{"voyage rerank map[string]string", "voyage", rerank(func() (provider.RerankingModel, error) {
			return voyage.New(voyage.Config{APIKey: "k", BaseURL: base}).RerankingModel("rerank-2.5")
		}), map[string]interface{}{"truncation": true, "returnDocuments": true}, `"return_documents":true`},
	}
	var failures []string
	for _, tcase := range cases {
		c.take()
		tcase.call(map[string]interface{}{tcase.ns: tcase.value})
		typed := c.take()
		tcase.call(map[string]interface{}{tcase.ns: r91integJSONForm(t, tcase.value)})
		plain := c.take()
		switch {
		case !strings.Contains(plain, tcase.want):
			failures = append(failures, fmt.Sprintf("%s: JSON form body lacks %s: %s", tcase.name, tcase.want, plain))
		case typed != plain:
			failures = append(failures, fmt.Sprintf("%s: typed body differs\n typed: %s\n plain: %s", tcase.name, typed, plain))
		}
	}
	for _, f := range failures {
		t.Error(f)
	}
}
