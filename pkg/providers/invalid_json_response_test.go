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
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// TS createJsonResponseHandler(schema): a 2xx body that does not match the
// provider's response schema is an APICallError "Invalid JSON response" whose
// cause is the TypeValidationError (or the JSONParseError for text that is
// not JSON). Status, headers and body ride along.
func TestNonStreamingResponseSchemaValidation(t *testing.T) {
	bodies := []struct {
		name, body string
		parse      bool
	}{
		{"choices-not-array", `{"id":"x","choices":"nope"}`, false},
		{"missing-choices", `{"id":"x"}`, false},
		{"not-json", `<html>gateway timeout</html>`, true},
	}
	for _, c := range keyCases() {
		for _, b := range bodies {
			t.Run(c.name+"/"+b.name, func(t *testing.T) {
				if c.name == "perplexity" || c.name == "huggingface" {
					t.Skip("schema not ported for this provider's chat API")
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Request-Id", "req-1")
					_, _ = w.Write([]byte(b.body))
				}))
				defer srv.Close()
				t.Setenv(c.env, "key")
				model := c.model(srv.URL, nil)
				_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
				var pe *providererrors.ProviderError
				if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" {
					t.Fatalf("error = %v, want ProviderError \"Invalid JSON response\"", err)
				}
				if pe.StatusCode != 200 || pe.ResponseBody != b.body || pe.ResponseHeaders["X-Request-Id"] != "req-1" {
					t.Fatalf("status/body/headers not carried: %d %q %v", pe.StatusCode, pe.ResponseBody, pe.ResponseHeaders)
				}
				if b.parse {
					var jp *providererrors.JSONParseError
					if !errors.As(err, &jp) {
						t.Fatalf("cause = %v, want JSONParseError", pe.Cause)
					}
				} else {
					var ve *providererrors.ValidationError
					if !errors.As(err, &ve) || !strings.Contains(ve.Error(), "Type validation failed") {
						t.Fatalf("cause = %v, want TypeValidationError", pe.Cause)
					}
				}
			})
		}
	}
}

func TestOpenAIResponsesAndCompletionSchemaValidation(t *testing.T) {
	cases := []struct {
		name, body string
		call       func(base string) error
	}{
		{"responses-output-not-array", `{"id":"r","output":"x"}`, func(base string) error {
			m, _ := openai.New(openai.Config{BaseURL: base, APIKey: "k"}).ResponsesModel("gpt-5")
			_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			return err
		}},
		{"responses-unknown-item", `{"id":"r","output":[{"type":"mystery"}]}`, func(base string) error {
			m, _ := openai.New(openai.Config{BaseURL: base, APIKey: "k"}).ResponsesModel("gpt-5")
			_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			return err
		}},
		{"responses-message-missing-text", `{"id":"r","output":[{"type":"message","role":"assistant","id":"m","content":[{"type":"output_text","annotations":[]}]}]}`, func(base string) error {
			m, _ := openai.New(openai.Config{BaseURL: base, APIKey: "k"}).ResponsesModel("gpt-5")
			_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			return err
		}},
		{"completion-choices-missing-text", `{"id":"c","choices":[{"finish_reason":"stop"}]}`, func(base string) error {
			m, _ := openai.New(openai.Config{BaseURL: base, APIKey: "k"}).CompletionModel("gpt-3.5-turbo-instruct")
			_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			err := c.call(srv.URL)
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" {
				t.Fatalf("error = %v, want ProviderError \"Invalid JSON response\"", err)
			}
		})
	}
}
