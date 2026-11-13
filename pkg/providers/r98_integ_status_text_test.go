package providers_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/bfl"
	"github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fal"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/luma"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/prodia"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
	"github.com/digitallysavvy/go-ai/pkg/providers/replicate"
	"github.com/digitallysavvy/go-ai/pkg/providers/stability"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// r98integReasonPhraseServer answers every request with
// "HTTP/1.1 <status> Custom Phrase" and a body outside every provider's
// error schema.
func r98integReasonPhraseServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close() //nolint:errcheck
		body := `<html>oops</html>`
		_, _ = fmt.Fprintf(buf, "HTTP/1.1 %d Custom Phrase\r\nContent-Type: text/html\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", status, len(body), body)
		_ = buf.Flush()
	}))
}

// TestR98integFailedResponseUsesReasonPhrase (integrator r98, provc CR part
// 1): TS createJsonErrorResponseHandler / createStatusCodeErrorResponseHandler
// fall back to response.statusText, the server's reason phrase, for a failed
// body outside the error schema. Go used http.StatusText(code). One
// representative call per provider family (the internal client's
// HTTPStatusError, raw-client responses, and the shared helpers).
func TestR98integFailedResponseUsesReasonPhrase(t *testing.T) {
	srv := r98integReasonPhraseServer(t, http.StatusBadRequest)
	defer srv.Close()

	models := map[string]func() (provider.LanguageModel, error){
		"alibaba (raw)": func() (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"anthropic": func() (provider.LanguageModel, error) {
			return anthropic.New(anthropic.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("claude-sonnet-4-5")
		},
		"bedrock (raw)": func() (provider.LanguageModel, error) {
			return bedrock.New(bedrock.Config{APIKey: "k", Region: "us-east-1", BaseURL: srv.URL}).LanguageModel("anthropic.claude-3-haiku-20240307-v1:0")
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
		"gmicloud": func() (provider.LanguageModel, error) {
			return gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"google": func() (provider.LanguageModel, error) {
			return google.New(google.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("gemini-2.5-flash")
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
		"openresponses": func() (provider.LanguageModel, error) {
			return openresponses.New(openresponses.Config{Name: "x", BaseURL: srv.URL}).LanguageModel("m")
		},
		"perplexity": func() (provider.LanguageModel, error) {
			return perplexity.New(perplexity.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("sonar")
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
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}}
	for name, mk := range models {
		t.Run(name, func(t *testing.T) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.DoGenerate(context.Background(), opts)
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.Message != "Custom Phrase" {
				t.Fatalf("DoGenerate err = %v, want a ProviderError with the reason phrase", err)
			}
		})
	}

	t.Run("voyage embedding", func(t *testing.T) {
		em, err := voyage.New(voyage.Config{APIKey: "k", BaseURL: srv.URL}).EmbeddingModel("voyage-3")
		if err != nil {
			t.Fatal(err)
		}
		_, err = em.DoEmbed(context.Background(), "hi", nil)
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Custom Phrase" {
			t.Fatalf("DoEmbed err = %v, want the reason phrase", err)
		}
	})

	images := map[string]func() (provider.ImageModel, error){
		"bfl image": func() (provider.ImageModel, error) {
			return bfl.New(bfl.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("flux-pro-1.1")
		},
		"bytedance image": func() (provider.ImageModel, error) {
			p, err := bytedance.New(bytedance.Config{APIKey: "k", BaseURL: srv.URL})
			if err != nil {
				return nil, err
			}
			return p.ImageModel("m")
		},
		"deepinfra image": func() (provider.ImageModel, error) {
			return deepinfra.New(deepinfra.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
		"fal image": func() (provider.ImageModel, error) {
			return fal.New(fal.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("fal-ai/m")
		},
		"fireworks image": func() (provider.ImageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
		"luma image": func() (provider.ImageModel, error) {
			return luma.New(luma.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("photon-1")
		},
		"minimax image": func() (provider.ImageModel, error) {
			return minimax.New(minimax.Config{APIKey: "k", ImageBaseURL: srv.URL}).ImageModel("image-01")
		},
		"prodia image": func() (provider.ImageModel, error) {
			return prodia.New(prodia.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
		"quiverai image": func() (provider.ImageModel, error) {
			return quiverai.New(quiverai.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
		"replicate image": func() (provider.ImageModel, error) {
			return replicate.New(replicate.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("o/m")
		},
		"stability image": func() (provider.ImageModel, error) {
			return stability.New(stability.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("sd3")
		},
		"together image": func() (provider.ImageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
		"xai image": func() (provider.ImageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("m")
		},
	}
	for name, mk := range images {
		t.Run(name, func(t *testing.T) {
			m, err := mk()
			if err != nil {
				t.Fatal(err)
			}
			_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "cat"})
			if err == nil || !strings.Contains(err.Error(), "Custom Phrase") {
				t.Fatalf("DoGenerate err = %v, want the reason phrase", err)
			}
		})
	}
}
