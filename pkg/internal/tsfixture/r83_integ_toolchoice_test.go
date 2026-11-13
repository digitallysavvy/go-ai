package tsfixture_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
)

// TestR83integUnknownToolChoiceTypeSiblings: the TS prepareTools exhaustive
// check (UnsupportedFunctionalityError "tool choice type: X") also exists in
// anthropic, google, groq, mistral, cohere and amazon-bedrock, and cerebras /
// deepinfra extend the openai-compatible chat model. Each fails before any
// request, in DoGenerate and DoStream; without tools nothing is checked.
func TestR83integUnknownToolChoiceTypeSiblings(t *testing.T) {
	var mu sync.Mutex
	sent := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		mu.Lock()
		sent++
		mu.Unlock()
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"x"}}`))
	}))
	defer srv.Close()
	const want = "'tool choice type: unrecognized' functionality not supported."
	models := []struct {
		n string
		f func() (provider.LanguageModel, error)
	}{
		{"anthropic", func() (provider.LanguageModel, error) {
			return anthropic.New(anthropic.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("claude-sonnet-4-5")
		}},
		{"google", func() (provider.LanguageModel, error) {
			return google.New(google.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("gemini-2.5-flash")
		}},
		{"groq", func() (provider.LanguageModel, error) {
			return groq.New(groq.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("llama-3.3-70b-versatile")
		}},
		{"mistral", func() (provider.LanguageModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("mistral-large-latest")
		}},
		{"cohere", func() (provider.LanguageModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("command-a-03-2025")
		}},
		{"bedrock", func() (provider.LanguageModel, error) {
			return bedrock.New(bedrock.Config{AWSAccessKeyID: "a", AWSSecretAccessKey: "s", Region: "us-east-1", BaseURL: srv.URL}).LanguageModel("amazon.nova-pro-v1:0")
		}},
		{"bedrock-anthropic", func() (provider.LanguageModel, error) {
			return bedrock.New(bedrock.Config{AWSAccessKeyID: "a", AWSSecretAccessKey: "s", Region: "us-east-1", BaseURL: srv.URL}).LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
		}},
		{"cerebras", func() (provider.LanguageModel, error) {
			return cerebras.New(cerebras.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("llama3.1-8b")
		}},
		{"deepinfra", func() (provider.LanguageModel, error) {
			return deepinfra.New(deepinfra.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		}},
		{"openai-chat", func() (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: srv.URL}).ChatModel("gpt-4o")
		}},
		{"together", func() (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		}},
		{"fireworks", func() (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("m")
		}},
	}
	tools := []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}}
	for _, c := range models {
		m, err := c.f()
		if err != nil {
			t.Fatal(c.n, err)
		}
		for _, stream := range []bool{false, true} {
			mu.Lock()
			sent = 0
			mu.Unlock()
			opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Tools: tools,
				ToolChoice: types.ToolChoice{Type: "unrecognized"}}
			if stream {
				var s provider.TextStream
				s, err = m.DoStream(context.Background(), opts)
				if s != nil {
					_ = s.Close()
				}
			} else {
				_, err = m.DoGenerate(context.Background(), opts)
			}
			var uf *providererrors.UnsupportedFunctionalityError
			if err == nil || err.Error() != want || !errors.As(err, &uf) {
				t.Errorf("%s stream=%v: err = %v, want %q", c.n, stream, err, want)
			}
			mu.Lock()
			if sent != 0 {
				t.Errorf("%s stream=%v: request sent", c.n, stream)
			}
			mu.Unlock()
		}
		// Without tools TS returns before the tool choice switch.
		_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
			ToolChoice: types.ToolChoice{Type: "unrecognized"}})
		if err != nil && err.Error() == want {
			t.Errorf("%s: no tools, still checked", c.n)
		}
	}
}

// TestR83integGroqJSONResponseToolReplacesUnknownChoice: TS groq replaces the
// tool choice for the JSON response tool (required unless tool or none)
// before prepareTools, so an unknown type is not checked then.
func TestR83integGroqJSONResponseToolReplacesUnknownChoice(t *testing.T) {
	var mu sync.Mutex
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = b
		mu.Unlock()
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"x"}}`))
	}))
	defer srv.Close()
	m, err := groq.New(groq.Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("llama-3.3-70b-versatile")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		Tools:          []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}},
		ToolChoice:     types.ToolChoice{Type: "unrecognized"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object"}}})
	var uf *providererrors.UnsupportedFunctionalityError
	if errors.As(err, &uf) {
		t.Fatalf("err = %v, want the request sent", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(string(body), `"tool_choice":"required"`) {
		t.Fatalf("body = %s, want tool_choice required", body)
	}
}
