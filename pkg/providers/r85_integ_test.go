package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type r85integCtxKey struct{}

// r85integKeyFuncs builds the language model of every provider whose
// APIKeyFunc / AuthTokenFunc is resolved through the internal/http
// request-time headers hook.
var r85integKeyFuncs = map[string]func(base string, f providerutils.APIKeyFunc) (provider.LanguageModel, error){
	"groq": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return groq.New(groq.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("m")
	},
	"mistral": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return mistral.New(mistral.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("m")
	},
	"cohere": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return cohere.New(cohere.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("m")
	},
	"perplexity": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return perplexity.New(perplexity.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("sonar")
	},
	"google": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return google.New(google.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("gemini-2.5-flash")
	},
	"anthropic authToken": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return anthropic.New(anthropic.Config{BaseURL: b, AuthTokenFunc: f}).LanguageModel("claude-sonnet-4-5")
	},
	"minimax": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return minimax.New(minimax.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("MiniMax-M2")
	},
	"openai": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return openai.New(openai.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("gpt-4o")
	},
	"azure": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		p, err := azure.New(azure.Config{BaseURL: b, APIKeyFunc: f})
		if err != nil {
			return nil, err
		}
		return p.LanguageModel("gpt-4o")
	},
	"xai": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return xai.New(xai.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("grok-4")
	},
	"deepseek": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return deepseek.New(deepseek.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("deepseek-chat")
	},
	"alibaba": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return alibaba.New(alibaba.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("qwen-plus")
	},
	"moonshot": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return moonshot.New(moonshot.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("kimi-k2")
	},
	"openresponses": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return openresponses.New(openresponses.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("m")
	},
	"huggingface": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return huggingface.New(huggingface.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("m")
	},
	"gmicloud": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return gmicloud.New(gmicloud.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("m")
	},
	"together": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return together.New(together.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("m")
	},
	"fireworks": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return fireworks.New(fireworks.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("m")
	},
	"zai": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return zai.New(zai.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("glm-4.6")
	},
	"cerebras": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return cerebras.New(cerebras.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("llama3.1-8b")
	},
	"deepinfra": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return deepinfra.New(deepinfra.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("m")
	},
	"quiverai": func(b string, f providerutils.APIKeyFunc) (provider.LanguageModel, error) {
		return quiverai.New(quiverai.Config{BaseURL: b, APIKeyFunc: f}).LanguageModel("")
	},
}

// The per-request credential func (TS 7716e189ee / 63bf860638) gets the
// caller's context: a value set on it is visible, and a cancelled context
// reaches the func, whose error fails the call before anything is sent.
// Round 85 moved every HeadersFunc-based APIKeyFunc to the context-aware
// internal/http HeadersFuncCtx (carry fd726a42); before, the func got
// context.Background().
func TestR85integAPIKeyFuncGetsCallerContext(t *testing.T) {
	for name, build := range r85integKeyFuncs {
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
			model, err := build(srv.URL, keyFunc)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), r85integCtxKey{}, "caller"))
			cancel()
			_, err = model.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("DoGenerate err = %v, want context.Canceled", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(seen) == 0 {
				t.Fatal("the key func was not called")
			}
			for _, v := range seen {
				if v != "caller" {
					t.Fatalf("key func ctx values = %v, want the caller's", seen)
				}
			}
			if n := hits.Load(); n != 0 {
				t.Fatalf("server got %d requests, want none", n)
			}
		})
	}
}
