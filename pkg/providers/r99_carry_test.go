package providers_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
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
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// r99carryNoContentServer answers every request with a status that fetch
// gives a null body (204 No Content or 205 Reset Content).
func r99carryNoContentServer(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "r1")
		w.WriteHeader(status)
	}))
}

// TestR99carryNullBodySuccessfulResponse: fetch gives a 204 / 205 response
// a null body (node 24). A TS stream call (createEventSourceResponseHandler)
// then throws EmptyResponseBodyError, which postToApi wraps in the
// APICallError "Failed to process successful response" with the status and
// response headers (not retryable); a TS generate call
// (createJsonResponseHandler) parses the empty text and throws the
// APICallError "Invalid JSON response". Go streamed nothing for the 204 / 205.
func TestR99carryNullBodySuccessfulResponse(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		srv := r99carryNoContentServer(status)
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
			"gateway": func() (provider.LanguageModel, error) {
				p, err := gateway.New(gateway.Config{APIKey: "k", BaseURL: srv.URL})
				if err != nil {
					return nil, err
				}
				return p.LanguageModel("openai/gpt-4o")
			},
			"zai": func() (provider.LanguageModel, error) {
				return zai.New(zai.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
			},
		}
		opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}}
		for name, mk := range models {
			t.Run(fmt.Sprintf("%s %d", name, status), func(t *testing.T) {
				m, err := mk()
				if err != nil {
					t.Fatal(err)
				}
				stream, err := m.DoStream(context.Background(), opts)
				if stream != nil {
					_ = stream.Close()
				}
				var pe *providererrors.ProviderError
				if !errors.As(err, &pe) || pe.Message != providererrors.FailedToProcessSuccessfulResponseMessage ||
					pe.StatusCode != status || !providererrors.IsEmptyResponseBodyError(err) || pe.IsRetryable() ||
					pe.ResponseHeader("x-request-id") != "r1" {
					t.Fatalf("DoStream err = %v (%+v), want the APICallError Failed to process successful response", err, pe)
				}
				_, err = m.DoGenerate(context.Background(), opts)
				if !errors.As(err, &pe) || pe.Message != providererrors.InvalidJSONResponseMessage || pe.StatusCode != status {
					t.Fatalf("DoGenerate err = %v, want the APICallError Invalid JSON response", err)
				}
			})
		}
	}
}
