package providers_test

import (
	"context"
	"errors"
	"fmt"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TS requires numeric `index` members on choices (openai chat/completion, groq,
// mistral, alibaba) and tool_calls (openai chat, groq, deepseek); absent index
// fails the zod parse. Providers whose schemas make them nullish or absent
// (deepseek/moonshot choices, compat chat) must keep accepting the frame.
func TestChatStreamIndexMembers(t *testing.T) {
	type mk func(u string) (provider.LanguageModel, error)
	openaiChat := func(u string) (provider.LanguageModel, error) {
		return openai.New(openai.Config{APIKey: "k", BaseURL: u}).ChatModel("gpt-4o")
	}
	groqM := func(u string) (provider.LanguageModel, error) {
		return groq.New(groq.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
	}
	deepseekM := func(u string) (provider.LanguageModel, error) {
		return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: u}).LanguageModel("deepseek-chat")
	}
	mistralM := func(u string) (provider.LanguageModel, error) {
		return mistral.New(mistral.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
	}
	alibabaM := func(u string) (provider.LanguageModel, error) {
		return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: u}).LanguageModel("qwen-plus")
	}
	moonshotM := func(u string) (provider.LanguageModel, error) {
		return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
	}
	togetherM := func(u string) (provider.LanguageModel, error) {
		return together.New(together.Config{APIKey: "k", BaseURL: u}).LanguageModel("m")
	}
	completion := func(u string) (provider.LanguageModel, error) {
		return openai.New(openai.Config{APIKey: "k", BaseURL: u}).CompletionModel("gpt-3.5-turbo-instruct")
	}
	noChoiceIdx := `{"choices":[{"delta":{"content":"a"}}]}`
	noCallIdx := `{"choices":[{"index":0,"delta":{"tool_calls":[{"id":"c","function":{"name":"f","arguments":"{}"}}]}}]}`
	noCallIdxNoChoiceIdx := `{"choices":[{"delta":{"tool_calls":[{"id":"c","function":{"name":"f","arguments":"{}"}}]}}]}`
	cases := []struct {
		name    string
		f       mk
		frame   string
		wantErr bool
	}{
		{"openai/choice", openaiChat, noChoiceIdx, true},
		{"openai/toolcall", openaiChat, noCallIdx, true},
		{"completion/choice", completion, `{"choices":[{"text":"a"}]}`, true},
		{"groq/choice", groqM, noChoiceIdx, true},
		{"groq/toolcall", groqM, noCallIdx, true},
		{"mistral/choice", mistralM, noChoiceIdx, true},
		{"alibaba/choice", alibabaM, noChoiceIdx, true},
		{"deepseek/choice-optional", deepseekM, noChoiceIdx, false},
		{"deepseek/toolcall", deepseekM, noCallIdxNoChoiceIdx, true},
		{"moonshot/choice-optional", moonshotM, noChoiceIdx, false},
		{"moonshot/toolcall-optional", moonshotM, noCallIdxNoChoiceIdx, false},
		{"together/choice-optional", togetherM, noChoiceIdx, false},
		{"together/toolcall-optional", togetherM, noCallIdxNoChoiceIdx, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\n", c.frame)
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer srv.Close()
			m, err := c.f(srv.URL)
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
				ch, err := s.Next()
				if err != nil {
					break
				}
				if ch.Type == provider.ChunkTypeError && providererrors.IsValidationError(ch.Err) && strings.HasPrefix(ch.Text, "Type validation failed") {
					sawError = true
				}
			}
			if sawError != c.wantErr {
				t.Fatalf("frame %s: error chunk = %v, want %v", c.frame, sawError, c.wantErr)
			}
		})
	}
}

// chatStreamProviders builds one model per OpenAI-family chat stream.
func chatStreamProviders() map[string]func(u string) (provider.LanguageModel, error) {
	return map[string]func(u string) (provider.LanguageModel, error){
		"openai-chat": func(u string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: u}).ChatModel("gpt-4o")
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
}

// streamErrorChunks streams frame (then a stop frame) and returns every error chunk.
func streamErrorChunks(t *testing.T, f func(u string) (provider.LanguageModel, error), frame string) []*provider.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
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
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeError {
			out = append(out, c)
		}
	}
	return out
}

// Scalar member types (content, role, usage inner types, created) are checked
// like the TS zod schemas, and every parse-failure error chunk carries the
// typed error (ValidationError / JSONParseError) with Text == Err.Error().
func TestChatStreamScalarMemberTypesAndTypedErrors(t *testing.T) {
	for pn, f := range chatStreamProviders() {
		pn, f := pn, f
		t.Run(pn+"/content-number", func(t *testing.T) {
			errs := streamErrorChunks(t, f, `{"choices":[{"index":0,"delta":{"content":5}}]}`)
			if len(errs) != 1 {
				t.Fatalf("want 1 error chunk, got %d", len(errs))
			}
			var ve *providererrors.ValidationError
			if !errors.As(errs[0].Err, &ve) || errs[0].Text != errs[0].Err.Error() {
				t.Fatalf("err = %T %v text=%q", errs[0].Err, errs[0].Err, errs[0].Text)
			}
		})
		t.Run(pn+"/invalid-json", func(t *testing.T) {
			errs := streamErrorChunks(t, f, `{"choices":[`)
			if len(errs) != 1 {
				t.Fatalf("want 1 error chunk, got %d", len(errs))
			}
			var je *providererrors.JSONParseError
			if !errors.As(errs[0].Err, &je) || errs[0].Text != errs[0].Err.Error() {
				t.Fatalf("err = %T %v", errs[0].Err, errs[0].Err)
			}
		})
		t.Run(pn+"/any-json-number", func(t *testing.T) {
			// zod z.number() accepts fractional and exponent forms.
			errs := streamErrorChunks(t, f, `{"created":1.5,"choices":[{"index":1.0,"delta":{"content":"a"}}],"usage":{"prompt_tokens":2.0,"completion_tokens":3,"total_tokens":5}}`)
			if len(errs) != 0 {
				t.Fatalf("valid TS frame rejected: %v", errs[0].Text)
			}
		})
	}
	t.Run("compat/content-array", func(t *testing.T) {
		for _, pn := range []string{"cerebras", "fireworks", "together", "ollama"} {
			errs := streamErrorChunks(t, chatStreamProviders()[pn], `{"choices":[{"delta":{"content":[{"type":"text","text":"a"}]}}]}`)
			if len(errs) != 0 {
				t.Fatalf("%s: valid openai-compatible frame rejected: %v", pn, errs[0].Text)
			}
		}
	})
}
