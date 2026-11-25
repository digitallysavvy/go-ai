package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
)

// TS createJsonErrorResponseHandler({errorSchema, errorToMessage}): a body
// that matches the provider's error schema gives errorToMessage(data) and the
// zod-parsed data (undeclared keys stripped); any other body gives the HTTP
// status text and no data. The generic Go path read many shapes and kept any
// JSON body as data (provg r89 integrator item). Fireworks images use
// createStatusCodeErrorResponseHandler (status text, no data).
func TestR89integErrorSchemaMessageAndData(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	prompt := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
	call := map[string]func() error{
		"cohere": func() error {
			m, _ := cohere.New(cohere.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("command-r")
			_, err := m.DoGenerate(context.Background(), prompt)
			return err
		},
		"mistral": func() error {
			m, _ := mistral.New(mistral.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("mistral-small-latest")
			_, err := m.DoGenerate(context.Background(), prompt)
			return err
		},
		"together-rerank": func() error {
			m, _ := together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).RerankingModel("Salesforce/Llama-Rank-v1")
			_, err := m.DoRerank(context.Background(), &provider.RerankOptions{Documents: []string{"a"}, Query: "q"})
			return err
		},
		"fireworks-image": func() error {
			m, _ := fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("accounts/fireworks/models/flux-1-dev-fp8")
			_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
			return err
		},
	}
	for _, tc := range []struct {
		name, body, msg string
		data            interface{}
	}{
		{"cohere", `{"message":"slow down","extra":1}`, "slow down", map[string]interface{}{"message": "slow down"}},
		{"cohere", `{"error":{"message":"x"}}`, "Bad Request", nil},
		{"mistral", `{"object":"error","message":"bad model","type":"invalid_request_error","param":null,"code":"1500","x":1}`, "bad model", map[string]interface{}{"object": "error", "message": "bad model", "type": "invalid_request_error", "param": nil, "code": "1500"}},
		{"mistral", `{"message":"no object key"}`, "Bad Request", nil},
		{"together-rerank", `{"error":{"message":"bad query","code":3}}`, "bad query", map[string]interface{}{"error": map[string]interface{}{"message": "bad query"}}},
		{"together-rerank", `{"message":"flat"}`, "Bad Request", nil},
		{"fireworks-image", `{"error":{"message":"nope"}}`, "Bad Request", nil},
	} {
		body = tc.body
		err := call[tc.name]()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("%s %s: err = %T %v", tc.name, tc.body, err, err)
		}
		if pe.Message != tc.msg || !reflect.DeepEqual(pe.Data, tc.data) {
			t.Fatalf("%s %s: message %q data %#v, want %q %#v", tc.name, tc.body, pe.Message, pe.Data, tc.msg, tc.data)
		}
		if pe.StatusCode != 400 || pe.ResponseBody != tc.body || pe.ResponseHeaders["Retry-After"] == "" && pe.ResponseHeaders["retry-after"] == "" {
			t.Fatalf("%s: status %d body %q headers %v", tc.name, pe.StatusCode, pe.ResponseBody, pe.ResponseHeaders)
		}
	}
}

// OpenAI-compatible wrappers whose TS model uses the default
// openai-compatible error structure (deepinfra, together completions) or
// fireworksErrorStructure give the schema message for a matching body and
// the status text otherwise; compat mode used the generic extraction
// (provb r89 open item, openai.Config.CompatErrorStructure).
func TestR89integCompatWrapperErrorStructure(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	prompt := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
	models := map[string]func() (provider.LanguageModel, error){
		"deepinfra": func() (provider.LanguageModel, error) {
			return deepinfra.New(deepinfra.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		},
		"together-completion": func() (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("m")
		},
		"fireworks-completion": func() (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).CompletionModel("m")
		},
	}
	for name, mk := range models {
		m, err := mk()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, tc := range []struct{ body, msg string }{
			{`{"error":{"message":"schema message","type":"t"}}`, "schema message"},
			{`{"message":"top-level message"}`, "Bad Request"},
		} {
			body = tc.body
			_, err := m.DoGenerate(context.Background(), prompt)
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.Message != tc.msg {
				t.Fatalf("%s %s: err = %v, want message %q", name, tc.body, err, tc.msg)
			}
		}
	}
}
