package providers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bfl"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/hume"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// r86integCapture records the first request a provider sends, normalized so
// that a JSON body compares by value and a multipart body by its fields.
type r86integCapture struct {
	mu   sync.Mutex
	reqs []string
}

func (c *r86integCapture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		norm := r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery + "\n" + r86integNormalizeBody(r.Header.Get("Content-Type"), body)
		c.mu.Lock()
		c.reqs = append(c.reqs, norm)
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop","type":"invalid_request_error"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *r86integCapture) first() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.reqs) == 0 {
		return "", false
	}
	return c.reqs[0], true
}

func r86integNormalizeBody(contentType string, body []byte) string {
	mediaType, params, _ := mime.ParseMediaType(contentType)
	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(strings.NewReader(string(body)), params["boundary"])
		var fields []string
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			v, _ := io.ReadAll(p)
			if p.FileName() != "" {
				v = []byte("<file>")
			}
			fields = append(fields, p.FormName()+"="+string(v))
		}
		sort.Strings(fields)
		return strings.Join(fields, "\n")
	}
	var v interface{}
	if json.Unmarshal(body, &v) == nil {
		b, _ := json.Marshal(v)
		return string(b)
	}
	return string(body)
}

// Class sweep, round 86 (provb #1-#5, prova #8 and the provb CRs): TS parses
// a provider option as its JSON value, so a Go-typed value ([]string,
// [][]interface{}, []map[string]interface{}, map[string]bool) must send the
// same request as its JSON form ([]interface{}, map[string]interface{}).
// Every case sends one request with each form; both must reach the server
// and be equal.
func TestR86integTypedProviderOptionsMatchJSONForm(t *testing.T) {
	ctx := context.Background()
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}}
	cases := []struct {
		name        string
		jsonForm    interface{}
		typed       interface{}
		mustContain string
		call        func(base string, opt interface{}) error
	}{
		{
			name: "groq transcription timestampGranularities", mustContain: "timestamp_granularities[]=word",
			jsonForm: []interface{}{"word"}, typed: []string{"word"},
			call: func(base string, opt interface{}) error {
				m, _ := groq.New(groq.Config{APIKey: "k", BaseURL: base}).TranscriptionModel("whisper-large-v3")
				_, err := m.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte("RIFF"), MimeType: "audio/wav",
					ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"timestampGranularities": opt}}})
				return err
			},
		},
		{
			name: "bfl video keyframes", mustContain: `"keyframes":["https://a.test/1.png","https://a.test/2.png"]`,
			jsonForm: []interface{}{"https://a.test/1.png", "https://a.test/2.png"}, typed: []string{"https://a.test/1.png", "https://a.test/2.png"},
			call: func(base string, opt interface{}) error {
				m, _ := bfl.New(bfl.Config{APIKey: "k", BaseURL: base}).VideoModel(bfl.VideoModelFlux3Video)
				_, err := m.DoGenerate(ctx, &provider.VideoModelV3CallOptions{Prompt: "p", PromptSet: true,
					ProviderOptions: map[string]interface{}{"blackForestLabs": map[string]interface{}{"keyframes": opt}}})
				return err
			},
		},
		{
			name: "bfl video timed keyframes", mustContain: `"keyframes":[[0,"https://a.test/1.png"],[2,"https://a.test/2.png"]]`,
			jsonForm: []interface{}{[]interface{}{0.0, "https://a.test/1.png"}, []interface{}{2.0, "https://a.test/2.png"}},
			typed:    [][]interface{}{{0, "https://a.test/1.png"}, {2, "https://a.test/2.png"}},
			call: func(base string, opt interface{}) error {
				m, _ := bfl.New(bfl.Config{APIKey: "k", BaseURL: base}).VideoModel(bfl.VideoModelFlux3Video)
				_, err := m.DoGenerate(ctx, &provider.VideoModelV3CallOptions{Prompt: "p", PromptSet: true,
					ProviderOptions: map[string]interface{}{"blackForestLabs": map[string]interface{}{"keyframes": opt}}})
				return err
			},
		},
		{
			name: "hume speech context utterances", mustContain: `"context":{"utterances":[{"description":"","text":"a"}]}`,
			jsonForm: []interface{}{map[string]interface{}{"text": "a", "description": ""}},
			typed:    []map[string]interface{}{{"text": "a", "description": ""}},
			call: func(base string, opt interface{}) error {
				m, _ := hume.New(hume.Config{APIKey: "k", BaseURL: base}).SpeechModel("")
				_, err := m.DoGenerate(ctx, &provider.SpeechGenerateOptions{Text: "hi",
					ProviderOptions: map[string]interface{}{"hume": map[string]interface{}{"context": map[string]interface{}{"utterances": opt}}}})
				return err
			},
		},
		{
			name: "google embedding content", mustContain: `{"text":"x"}`,
			jsonForm: []interface{}{[]interface{}{map[string]interface{}{"text": "x"}}},
			typed:    [][]map[string]interface{}{{{"text": "x"}}},
			call: func(base string, opt interface{}) error {
				m, _ := google.New(google.Config{APIKey: "k", BaseURL: base}).EmbeddingModel("gemini-embedding-2")
				_, err := m.DoEmbed(ctx, "hi", &provider.EmbedModelOptions{
					ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"content": opt}}})
				return err
			},
		},
		{
			name: "anthropic tool allowedCallers", mustContain: `"allowed_callers":["code_execution_20260120"]`,
			jsonForm: []interface{}{"code_execution_20260120"}, typed: []string{"code_execution_20260120"},
			call: func(base string, opt interface{}) error {
				m, _ := anthropic.New(anthropic.Config{APIKey: "k", BaseURL: base}).LanguageModel("claude-sonnet-4-5")
				_, err := m.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: []types.Tool{{
					Name: "f", Parameters: schema,
					ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"allowedCallers": opt}},
				}}})
				return err
			},
		},
		{
			name: "google interactions environment sources", mustContain: `"sources":[{"content":"x","target":"/a","type":"inline"}]`,
			jsonForm: []interface{}{map[string]interface{}{"type": "inline", "content": "x", "target": "/a"}},
			typed:    []map[string]interface{}{{"type": "inline", "content": "x", "target": "/a"}},
			call: func(base string, opt interface{}) error {
				m, _ := google.New(google.Config{APIKey: "k", BaseURL: base}).InteractionsAgent("deep-research-pro-preview-12-2025")
				_, err := m.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
					ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"environment": map[string]interface{}{"type": "remote", "sources": opt}}}})
				return err
			},
		},
		{
			name: "moonshot system message tools", mustContain: `"name":"calc"`,
			jsonForm: []interface{}{map[string]interface{}{"type": "function", "name": "calc", "description": "d", "inputSchema": schema}},
			typed:    []map[string]interface{}{{"type": "function", "name": "calc", "description": "d", "inputSchema": schema}},
			call: func(base string, opt interface{}) error {
				m, _ := moonshot.New(moonshot.Config{APIKey: "k", BaseURL: base}).LanguageModel("kimi-k3")
				_, err := m.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
					{Role: types.RoleSystem, ProviderOptions: map[string]interface{}{"moonshotai": map[string]interface{}{"tools": opt}}},
					{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
				}}})
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var wantCap, gotCap r86integCapture
			jsonErr := tc.call(wantCap.server(t).URL, tc.jsonForm)
			want, ok := wantCap.first()
			if !ok {
				t.Fatalf("JSON form sent no request: %v", jsonErr)
			}
			if !strings.Contains(want, tc.mustContain) {
				t.Fatalf("JSON form request lacks %s:\n%s", tc.mustContain, want)
			}
			typedErr := tc.call(gotCap.server(t).URL, tc.typed)
			got, ok := gotCap.first()
			if !ok {
				t.Fatalf("typed form sent no request: %v", typedErr)
			}
			if got != want {
				t.Fatalf("typed form request:\n%s\nJSON form request:\n%s", got, want)
			}
		})
	}
}

// r86integKeyFuncCalls lists, per provider and request kind, one call made
// with an APIKeyFunc (or AuthTokenFunc). Generate requests are covered for
// every language model by TestR85integAPIKeyFuncGetsCallerContext.
func r86integKeyFuncCalls() map[string]func(ctx context.Context, base string, f providerutils.APIKeyFunc) error {
	lm := func(build func(string, providerutils.APIKeyFunc) (provider.LanguageModel, error)) func(context.Context, string, providerutils.APIKeyFunc) error {
		return func(ctx context.Context, base string, f providerutils.APIKeyFunc) error {
			m, err := build(base, f)
			if err != nil {
				return err
			}
			_, err = m.DoStream(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			return err
		}
	}
	embed := func(m provider.EmbeddingModel, err error) func(context.Context) error {
		return func(ctx context.Context) error {
			if err != nil {
				return err
			}
			_, err := m.DoEmbed(ctx, "hi", &provider.EmbedModelOptions{})
			return err
		}
	}
	image := func(m provider.ImageModel, err error) func(context.Context) error {
		return func(ctx context.Context) error {
			if err != nil {
				return err
			}
			_, err := m.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p"})
			return err
		}
	}
	speech := func(m provider.SpeechModel, err error) func(context.Context) error {
		return func(ctx context.Context) error {
			if err != nil {
				return err
			}
			_, err := m.DoGenerate(ctx, &provider.SpeechGenerateOptions{Text: "hi"})
			return err
		}
	}
	transcribe := func(m provider.TranscriptionModel, err error) func(context.Context) error {
		return func(ctx context.Context) error {
			if err != nil {
				return err
			}
			_, err := m.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte("RIFF"), MimeType: "audio/wav"})
			return err
		}
	}
	rerank := func(m provider.RerankingModel, err error) func(context.Context) error {
		return func(ctx context.Context) error {
			if err != nil {
				return err
			}
			_, err := m.DoRerank(ctx, &provider.RerankOptions{Documents: []string{"a"}, Query: "q"})
			return err
		}
	}
	video := func(m provider.VideoModelV3, err error) func(context.Context) error {
		return func(ctx context.Context) error {
			if err != nil {
				return err
			}
			_, err := m.DoGenerate(ctx, &provider.VideoModelV3CallOptions{Prompt: "p", PromptSet: true})
			return err
		}
	}
	upload := func(api provider.FilesAPI) func(context.Context) error {
		return func(ctx context.Context) error {
			_, err := api.UploadFile(ctx, types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}, MediaType: "text/plain", Filename: "a.txt"})
			return err
		}
	}
	batch := func(b provider.BatchV4) func(context.Context) error {
		return func(ctx context.Context) error {
			_, err := b.DoGetBatchStatus(ctx, provider.BatchV4OperationOptions{BatchID: "b"})
			return err
		}
	}
	type tokener interface {
		GetRealtimeToken(context.Context, provider.RealtimeFactoryGetTokenOptions) (provider.ClientSecretResult, error)
	}
	token := func(p tokener, model string) func(context.Context) error {
		return func(ctx context.Context) error {
			_, err := p.GetRealtimeToken(ctx, provider.RealtimeFactoryGetTokenOptions{Model: model})
			return err
		}
	}
	calls := map[string]func(ctx context.Context, base string, f providerutils.APIKeyFunc) error{}
	for name, build := range r85integKeyFuncs {
		calls["stream/"+name] = lm(build)
	}
	add := func(name string, mk func(base string, f providerutils.APIKeyFunc) func(context.Context) error) {
		calls[name] = func(ctx context.Context, base string, f providerutils.APIKeyFunc) error { return mk(base, f)(ctx) }
	}
	add("embed/openai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return embed(openai.New(openai.Config{BaseURL: b, APIKeyFunc: f}).EmbeddingModel("text-embedding-3-small"))
	})
	add("embed/google", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return embed(google.New(google.Config{BaseURL: b, APIKeyFunc: f}).EmbeddingModel("gemini-embedding-001"))
	})
	add("embed/mistral", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return embed(mistral.New(mistral.Config{BaseURL: b, APIKeyFunc: f}).EmbeddingModel("mistral-embed"))
	})
	add("embed/cohere", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return embed(cohere.New(cohere.Config{BaseURL: b, APIKeyFunc: f}).EmbeddingModel("embed-v4.0"))
	})
	add("embed/voyage", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return embed(voyage.New(voyage.Config{BaseURL: b, APIKeyFunc: f}).EmbeddingModel("voyage-3"))
	})
	add("embed/together", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return embed(together.New(together.Config{BaseURL: b, APIKeyFunc: f}).EmbeddingModel("m"))
	})
	add("image/openai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return image(openai.New(openai.Config{BaseURL: b, APIKeyFunc: f}).ImageModel("gpt-image-1"))
	})
	add("image/xai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return image(xai.New(xai.Config{BaseURL: b, APIKeyFunc: f}).ImageModel("grok-2-image"))
	})
	add("image/deepinfra", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return image(deepinfra.New(deepinfra.Config{BaseURL: b, APIKeyFunc: f}).ImageModel("m"))
	})
	add("image/together", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return image(together.New(together.Config{BaseURL: b, APIKeyFunc: f}).ImageModel("m"))
	})
	add("image/fireworks", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return image(fireworks.New(fireworks.Config{BaseURL: b, APIKeyFunc: f}).ImageModel("accounts/fireworks/models/flux-1-dev-fp8"))
	})
	add("image/google", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return image(google.New(google.Config{BaseURL: b, APIKeyFunc: f}).ImageModel("gemini-2.5-flash-image"))
	})
	add("speech/openai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return speech(openai.New(openai.Config{BaseURL: b, APIKeyFunc: f}).SpeechModel("tts-1"))
	})
	add("speech/elevenlabs", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return speech(elevenlabs.New(elevenlabs.Config{BaseURL: b, APIKeyFunc: f}).SpeechModel("eleven_v3"))
	})
	add("speech/xai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return speech(xai.New(xai.Config{BaseURL: b, APIKeyFunc: f}).SpeechModel("grok-tts"))
	})
	add("transcription/openai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return transcribe(openai.New(openai.Config{BaseURL: b, APIKeyFunc: f}).TranscriptionModel("whisper-1"))
	})
	add("transcription/groq", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return transcribe(groq.New(groq.Config{BaseURL: b, APIKeyFunc: f}).TranscriptionModel("whisper-large-v3"))
	})
	add("transcription/elevenlabs", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return transcribe(elevenlabs.New(elevenlabs.Config{BaseURL: b, APIKeyFunc: f}).TranscriptionModel("scribe_v1"))
	})
	add("transcription/mistral", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return transcribe(mistral.New(mistral.Config{BaseURL: b, APIKeyFunc: f}).TranscriptionModel("voxtral-mini-latest"))
	})
	add("transcription/xai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return transcribe(xai.New(xai.Config{BaseURL: b, APIKeyFunc: f}).TranscriptionModel("grok-stt"))
	})
	add("rerank/cohere", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return rerank(cohere.New(cohere.Config{BaseURL: b, APIKeyFunc: f}).RerankingModel("rerank-v3.5"))
	})
	add("rerank/voyage", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return rerank(voyage.New(voyage.Config{BaseURL: b, APIKeyFunc: f}).RerankingModel("rerank-2"))
	})
	add("video/xai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return video(xai.New(xai.Config{BaseURL: b, APIKeyFunc: f}).VideoModel("grok-imagine-video"))
	})
	add("video/google", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return video(google.New(google.Config{BaseURL: b, APIKeyFunc: f}).VideoModel("veo-3.0-generate-001"))
	})
	add("video/minimax", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return video(minimax.New(minimax.Config{BaseURL: b, APIKeyFunc: f}).VideoModel("MiniMax-Hailuo-02"))
	})
	add("files/openai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return upload(openai.New(openai.Config{BaseURL: b, APIKeyFunc: f}).Files())
	})
	add("files/anthropic", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return upload(anthropic.New(anthropic.Config{BaseURL: b, APIKeyFunc: f}).Files())
	})
	add("files/google", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return upload(google.New(google.Config{BaseURL: b, APIKeyFunc: f}).Files())
	})
	add("files/xai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return upload(xai.New(xai.Config{BaseURL: b, APIKeyFunc: f}).Files())
	})
	add("batch/openai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return batch(openai.New(openai.Config{BaseURL: b, APIKeyFunc: f}).ExperimentalBatch())
	})
	add("batch/anthropic", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return batch(anthropic.New(anthropic.Config{BaseURL: b, APIKeyFunc: f}).ExperimentalBatch())
	})
	add("batch/google", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return batch(google.New(google.Config{BaseURL: b, APIKeyFunc: f}).ExperimentalBatch())
	})
	add("batch/xai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return batch(xai.New(xai.Config{BaseURL: b, APIKeyFunc: f}).ExperimentalBatch())
	})
	add("realtime token/openai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return token(openai.New(openai.Config{BaseURL: b, APIKeyFunc: f}), "gpt-realtime")
	})
	add("realtime token/xai", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return token(xai.New(xai.Config{BaseURL: b, APIKeyFunc: f}), "grok-voice")
	})
	add("realtime token/google", func(b string, f providerutils.APIKeyFunc) func(context.Context) error {
		return token(google.New(google.Config{BaseURL: b, APIKeyFunc: f}), "gemini-live-2.5-flash-preview")
	})
	return calls
}

// Class sweep, round 86 (prova #3-#5, CRs to tools and provb): every
// provider request kind calls the per-request credential func (TS
// 7716e189ee) exactly once, with the caller's context: a value set on it is
// visible, and a cancelled context reaches the func, whose error fails the
// call before anything is sent.
func TestR86integAPIKeyFuncOncePerRequestKind(t *testing.T) {
	for name, call := range r86integKeyFuncCalls() {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				http.Error(w, "unexpected", http.StatusTeapot)
			}))
			defer srv.Close()
			var mu sync.Mutex
			var seen []interface{}
			keyFunc := func(ctx context.Context) (string, error) {
				mu.Lock()
				seen = append(seen, ctx.Value(r85integCtxKey{}))
				mu.Unlock()
				if err := ctx.Err(); err != nil {
					return "", err
				}
				return "k", nil
			}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), r85integCtxKey{}, "caller"))
			cancel()
			err := call(ctx, srv.URL, keyFunc)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(seen) != 1 || seen[0] != "caller" {
				t.Fatalf("key func calls (ctx values) = %v, want one with the caller's", seen)
			}
			if n := hits.Load(); n != 0 {
				t.Fatalf("server got %d requests, want none", n)
			}
		})
	}
}

// prova CR r86 2 (to provb, applied by the integrator): the same-origin
// download of a URL-only OpenAI-compatible image resolves the provider
// headers with the call's ctx (TS config.headers() inside the call), so the
// APIKeyFunc sees the caller's value on both requests; it ran with
// context.Background() through Client.Headers().
func TestR86integCompatImageDownloadKeyFuncGetsCallerCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/generations" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"url":"http://` + r.Host + `/img.png"}]}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte{137, 80, 78, 71})
	}))
	defer srv.Close()
	var mu sync.Mutex
	var seen []interface{}
	keyFunc := func(ctx context.Context) (string, error) {
		mu.Lock()
		seen = append(seen, ctx.Value(r85integCtxKey{}))
		mu.Unlock()
		return "k", nil
	}
	m, err := openai.New(openai.Config{APIKeyFunc: keyFunc, Name: "acme", BaseURL: srv.URL + "/v1", OpenAICompatible: true}).ImageModel("m")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), r85integCtxKey{}, "caller")
	res, err := m.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "x"})
	if err != nil || len(res.Images) != 1 {
		t.Fatalf("res=%v err=%v", res, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("key func calls = %v, want one per request (2)", seen)
	}
	for _, v := range seen {
		if v != "caller" {
			t.Fatalf("key func ctx values = %v, want the caller's", seen)
		}
	}
}
