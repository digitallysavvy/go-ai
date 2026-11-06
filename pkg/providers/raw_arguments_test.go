package providers_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/baseten"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/vercel"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// TS stringifies the parsed tool-call arguments, which keeps the wire key
// order; Go keeps the wire text as ToolCall.RawArguments so history replay
// (and the ToolCallContent input) keep that order. Every chat-completions
// provider has to carry it, for generate and stream.
func TestChatCompletionsToolCallsKeepRawArguments(t *testing.T) {
	const args = `{"zeta":1,"alpha":{"yy":1,"bb":2}}`
	const escaped = `{\"zeta\":1,\"alpha\":{\"yy\":1,\"bb\":2}}`
	generateBody := `{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"t","arguments":"` + escaped + `"}}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	streamBody := "data: " + `{"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"t","arguments":"` + escaped + `"}}]}}]}` + "\n\n" +
		"data: " + `{"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n" + "data: [DONE]\n\n"

	lm := func(m provider.LanguageModel, err error) provider.LanguageModel {
		if err != nil {
			panic(err)
		}
		return m
	}
	cases := map[string]func(u string) provider.LanguageModel{
		"openai": func(u string) provider.LanguageModel {
			return lm(openai.New(openai.Config{APIKey: "k", BaseURL: u}).ChatModel("gpt-4o"))
		},
		"moonshot": func(u string) provider.LanguageModel {
			return lm(moonshot.New(moonshot.Config{APIKey: "k", BaseURL: u}).LanguageModel("kimi-k2.5"))
		},
		"mistral": func(u string) provider.LanguageModel {
			return lm(mistral.New(mistral.Config{APIKey: "k", BaseURL: u}).LanguageModel("mistral-large-latest"))
		},
		"alibaba": func(u string) provider.LanguageModel {
			return lm(alibaba.New(alibaba.Config{APIKey: "k", BaseURL: u}).LanguageModel("qwen-plus"))
		},
		"deepseek": func(u string) provider.LanguageModel {
			return lm(deepseek.New(deepseek.Config{APIKey: "k", BaseURL: u}).LanguageModel("deepseek-chat"))
		},
		"groq": func(u string) provider.LanguageModel {
			return lm(groq.New(groq.Config{APIKey: "k", BaseURL: u}).LanguageModel("llama"))
		},
		"together": func(u string) provider.LanguageModel {
			return lm(together.New(together.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		},
		"fireworks": func(u string) provider.LanguageModel {
			return lm(fireworks.New(fireworks.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		},
		"cerebras": func(u string) provider.LanguageModel {
			return lm(cerebras.New(cerebras.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		},
		"deepinfra": func(u string) provider.LanguageModel {
			return lm(deepinfra.New(deepinfra.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		},
		"zai": func(u string) provider.LanguageModel {
			return lm(zai.New(zai.Config{APIKey: "k", BaseURL: u}).LanguageModel("glm-4.6"))
		},
		"gmicloud": func(u string) provider.LanguageModel {
			return lm(gmicloud.New(gmicloud.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		},
		"baseten": func(u string) provider.LanguageModel {
			return lm(baseten.New(baseten.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		},
		"vercel": func(u string) provider.LanguageModel {
			return lm(vercel.New(vercel.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				if strings.Contains(string(b), `"stream":true`) {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte(streamBody))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(generateBody))
			}))
			defer srv.Close()
			m := mk(srv.URL)
			opts := &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "hi"},
				Tools:  []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}},
			}
			res, err := m.DoGenerate(context.Background(), opts)
			if err != nil || len(res.ToolCalls) != 1 {
				t.Fatalf("generate: err=%v calls=%d", err, len(res.ToolCalls))
			}
			if got := res.ToolCalls[0].RawArguments; got != args {
				t.Errorf("generate RawArguments = %q, want %q", got, args)
			}
			stream, err := m.DoStream(context.Background(), opts)
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			defer stream.Close() //nolint:errcheck
			var got *types.ToolCall
			for {
				chunk, err := stream.Next()
				if err != nil {
					break
				}
				if chunk.Type == provider.ChunkTypeToolCall && chunk.ToolCall != nil {
					got = chunk.ToolCall
				}
			}
			if got == nil {
				t.Fatalf("stream: no tool call chunk")
			}
			if got.RawArguments != args {
				t.Errorf("stream RawArguments = %q, want %q", got.RawArguments, args)
			}
		})
	}
}
