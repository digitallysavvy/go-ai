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
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
)

// The TS chat chunk schemas (zod) require a `choices` array on every
// non-error frame; a frame that fails the parse becomes an error part and the
// finish reason turns to "error" (a later stop frame may overwrite it, as in TS). A JSON-valid frame of the wrong shape must
// do the same in Go.
func TestChatStreamChunkSchemaValidation(t *testing.T) {
	type mk func(u string) (provider.LanguageModel, error)
	provs := map[string]mk{
		"openai-chat": func(u string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: u}).ChatModel("gpt-4o")
		},
		"openai-completion": func(u string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: u}).CompletionModel("gpt-3.5-turbo-instruct")
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
		"together": func(u string) (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		},
		"fireworks": func(u string) (provider.LanguageModel, error) {
			return fireworks.New(fireworks.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		},
		"cerebras": func(u string) (provider.LanguageModel, error) {
			return cerebras.New(cerebras.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		},
		"ollama": func(u string) (provider.LanguageModel, error) {
			return ollama.New(ollama.Config{BaseURL: u}).LanguageModel("m")
		},
		"moonshot": func(u string) (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
		},
		"alibaba": func(u string) (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: u}).LanguageModel("qwen-plus")
		},
	}
	bad := map[string]string{
		"no-choices":  `{"foo":1}`,
		"empty-obj":   `{}`,
		"id-only":     `{"id":"x"}`,
		"choices-str": `{"choices":"x"}`,
	}
	for pn, f := range provs {
		for bn, frame := range bad {
			pn, f, bn, frame := pn, f, bn, frame
			t.Run(pn+"/"+bn, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
					if pn == "openai-completion" {
						_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"text\":\"\",\"finish_reason\":\"stop\",\"index\":0}]}\n\n")
					} else {
						_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}]}\n\n")
					}
					_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				}))
				defer srv.Close()
				m, err := f(srv.URL)
				if err != nil {
					t.Fatal(err)
				}
				s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close() //nolint:errcheck
				sawError := false
				for {
					c, err := s.Next()
					if err != nil {
						break
					}
					if c.Type == provider.ChunkTypeError {
						sawError = true
					}
				}
				if !sawError {
					t.Fatalf("frame %s produced no error chunk", frame)
				}
			})
		}
	}
}
