package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// prova r88 open item, class sweep (TS createJsonErrorResponseHandler ->
// safeParseJSON / secureJsonParse): an error body holding a __proto__ key
// does not parse, so its message is not used (status text) and no error data
// is kept. The same body without the key yields the message, so the probe
// reaches each provider's error parser.
func TestR88integErrorBodiesParseSecurely(t *testing.T) {
	const marker = "r88integ-error-message"
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	models := map[string]func() (provider.LanguageModel, error){
		"alibaba": func() (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"cerebras": func() (provider.LanguageModel, error) {
			return cerebras.New(cerebras.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"cohere": func() (provider.LanguageModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"deepseek": func() (provider.LanguageModel, error) {
			return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"fireworks": func() (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"gateway": func() (provider.LanguageModel, error) {
			p, err := gateway.New(gateway.Config{APIKey: "k", BaseURL: srv.URL})
			if err != nil {
				return nil, err
			}
			return p.LanguageModel("m")
		},
		"gmicloud": func() (provider.LanguageModel, error) {
			return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"google": func() (provider.LanguageModel, error) {
			return google.New(google.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"groq": func() (provider.LanguageModel, error) {
			return groq.New(groq.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"huggingface": func() (provider.LanguageModel, error) {
			return huggingface.New(huggingface.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"mistral": func() (provider.LanguageModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"moonshot": func() (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"openai": func() (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("gpt-4o")
		},
		"openai-resp": func() (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
		},
		"perplexity": func() (provider.LanguageModel, error) {
			return perplexity.New(perplexity.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"together": func() (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"xai": func() (provider.LanguageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"zai": func() (provider.LanguageModel, error) {
			return zai.New(zai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
	}
	// Probe bodies for the error schema families ({error:{message,type,code,
	// param}} with a numeric or string code, plus the flat shapes cerebras /
	// zai / perplexity / google read); the first that yields the message is
	// poisoned with a __proto__ key.
	probes := []string{
		`{"error":{"message":"` + marker + `","type":"invalid_request_error","code":400,"param":"p","status":"INVALID_ARGUMENT"},"message":"` + marker + `","type":"t","code":"c","param":"p"}`,
		`{"error":{"message":"` + marker + `","type":"invalid_request_error","code":"bad","param":"p"}}`,
		// mistral-error.ts: {object: "error", message, type, param, code} (r89).
		`{"object":"error","message":"` + marker + `","type":"t","param":null,"code":null}`,
	}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}}
	for name, mk := range models {
		t.Run(name, func(t *testing.T) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			message := func(err error) string {
				var pe *providererrors.ProviderError
				if errors.As(err, &pe) {
					return pe.Message
				}
				if err == nil {
					return ""
				}
				return err.Error()
			}
			clean := ""
			for _, probe := range probes {
				body = probe
				if _, cleanErr := m.DoGenerate(context.Background(), opts); strings.Contains(message(cleanErr), marker) {
					clean = probe
					break
				}
			}
			if clean == "" {
				t.Fatal("no probe body matches this provider's error schema")
			}
			body = strings.TrimSuffix(clean, "}") + `,"__proto__":{"x":1}}`
			_, err = m.DoGenerate(context.Background(), opts)
			if err == nil || strings.Contains(message(err), marker) {
				t.Fatalf("error from a __proto__ body used its message: %q", message(err))
			}
			var pe *providererrors.ProviderError
			if errors.As(err, &pe) && pe.Data != nil {
				t.Fatalf("error data kept for a __proto__ body: %#v", pe.Data)
			}
		})
	}
}
