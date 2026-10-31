package providers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

// TS emits `{type: 'raw', rawValue: chunk.rawValue}` for every SSE event, and
// safeParseJSON leaves rawValue undefined when the payload is not valid JSON.
// Each SSE provider's raw chunk for an unparsable payload must carry nil, not
// the payload text.
func TestR54IntegratorRawChunkForUnparsablePayloadIsNilPerProvider(t *testing.T) {
	type mk func(u string) (provider.LanguageModel, error)
	provs := map[string]mk{
		"openai-chat": func(u string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: u}).ChatModel("gpt-4o")
		},
		"openai-completion": func(u string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: u}).CompletionModel("gpt-3.5-turbo-instruct")
		},
		"openai-responses": func(u string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: u}).ResponsesModel("gpt-4o")
		},
		"groq": func(u string) (provider.LanguageModel, error) {
			return groq.New(groq.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		},
		"deepseek": func(u string) (provider.LanguageModel, error) {
			return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: u}).LanguageModel("deepseek-chat")
		},
		"mistral": func(u string) (provider.LanguageModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		},
		"mistral-conversation": func(u string) (provider.LanguageModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: u}).ConversationModel("m"), nil
		},
		"moonshot": func(u string) (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		},
		"alibaba": func(u string) (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: u}).LanguageModel("qwen-plus")
		},
		"together": func(u string) (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		},
		"cohere": func(u string) (provider.LanguageModel, error) {
			return cohere.New(cohere.Config{APIKey: "k", BaseURL: u}).LanguageModel("command-r")
		},
		"anthropic": func(u string) (provider.LanguageModel, error) {
			return anthropic.New(anthropic.Config{APIKey: "k", BaseURL: u}).LanguageModel("claude-sonnet-4-5")
		},
		"google": func(u string) (provider.LanguageModel, error) {
			return google.New(google.Config{APIKey: "k", BaseURL: u}).LanguageModel("gemini-2.5-flash")
		},
		"xai": func(u string) (provider.LanguageModel, error) {
			return xai.New(xai.Config{APIKey: "k", BaseURL: u}).LanguageModel("grok-4")
		},
		"openresponses": func(u string) (provider.LanguageModel, error) {
			return openresponses.New(openresponses.Config{BaseURL: u}).LanguageModel("m")
		},
	}
	for pn, f := range provs {
		t.Run(pn, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if pn == "anthropic" {
					// Anthropic reads the first event before DoStream returns.
					_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"model\":\"x\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
				}
				_, _ = fmt.Fprint(w, "data: not json\n\ndata: [DONE]\n\n")
			}))
			defer srv.Close()
			m, err := f(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, IncludeRawChunks: true})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close() //nolint:errcheck
			var raws []interface{}
			for i := 0; i < 100; i++ {
				c, err := s.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeRaw {
					raws = append(raws, c.Raw)
				}
			}
			sawNil := false
			for _, r := range raws {
				if r == nil {
					sawNil = true
				}
				if str, ok := r.(string); ok && str == "not json" {
					t.Fatalf("raw value for the unparsable payload = %q, want nil", str)
				}
			}
			if !sawNil {
				t.Fatalf("raw values = %#v, want a nil one for the unparsable payload", raws)
			}
		})
	}
}
