package providers_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
)

// r81integSSE builds a chat completion stream from delta JSON objects.
func r81integSSE(deltas ...string) string {
	var b strings.Builder
	for _, d := range deltas {
		b.WriteString(`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":` + d + `,"finish_reason":null}]}` + "\n\n")
	}
	b.WriteString(`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\ndata: [DONE]\n\n")
	return b.String()
}

func r81integStream(t *testing.T, sse string, build func(base string) (provider.LanguageModel, error)) []*provider.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()
	m, err := build(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	var chunks []*provider.StreamChunk
	for {
		c, err := st.Next()
		if errors.Is(err, io.EOF) {
			return chunks
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		chunks = append(chunks, c)
	}
}

// TS StreamingToolCallTracker.processNewToolCall throws "Expected
// 'function.name' to be a string." the moment a new call has no
// function.name, and the provider transforms (openai, groq, alibaba,
// deepseek, moonshotai, mistral) do not catch it: the stream ends with that
// error. No later delta names the call, no later part, tool call or finish
// follows. openai-compatible buffers an indexed nameless delta
// (pendingToolCalls) until its name arrives, but an index-less one goes to
// the tracker directly and throws too.
func TestR81integNamelessToolCallEndsStream(t *testing.T) {
	nameless := `{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"arguments":"{\"a\":"}}]}`
	named := `{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"t","arguments":"1}"}}]}`
	later := `{"content":"late"}`
	native := map[string]func(base string) (provider.LanguageModel, error){
		"openai": func(b string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: b}).ChatModel("gpt-4o")
		},
		"groq": func(b string) (provider.LanguageModel, error) {
			return groq.New(groq.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
		"alibaba": func(b string) (provider.LanguageModel, error) {
			return alibaba.New(alibaba.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
		"deepseek": func(b string) (provider.LanguageModel, error) {
			return deepseek.New(deepseek.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
		"moonshot": func(b string) (provider.LanguageModel, error) {
			return moonshot.New(moonshot.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
		"mistral": func(b string) (provider.LanguageModel, error) {
			return mistral.New(mistral.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
	}
	compat := map[string]func(base string) (provider.LanguageModel, error){
		"openai-compatible": func(b string) (provider.LanguageModel, error) {
			return openai.New(openai.Config{APIKey: "k", BaseURL: b, OpenAICompatible: true, Name: "acme"}).ChatModel("m")
		},
		"together": func(b string) (provider.LanguageModel, error) {
			return together.New(together.Config{APIKey: "k", BaseURL: b}).LanguageModel("m")
		},
	}
	assertEndsWithNameError := func(t *testing.T, chunks []*provider.StreamChunk) {
		t.Helper()
		if len(chunks) == 0 {
			t.Fatal("no chunks")
		}
		last := chunks[len(chunks)-1]
		var invalid *providererrors.InvalidResponseDataError
		if last.Type != provider.ChunkTypeError || last.Text != "Expected 'function.name' to be a string." || !errors.As(last.Err, &invalid) {
			t.Fatalf("last chunk = %+v, want the function.name error", last)
		}
		for _, c := range chunks[:len(chunks)-1] {
			switch c.Type {
			case provider.ChunkTypeError, provider.ChunkTypeFinish, provider.ChunkTypeToolCall, provider.ChunkTypeToolInputStart:
				t.Fatalf("chunk before the error: %+v", c)
			case provider.ChunkTypeText:
				if c.Text == "late" {
					t.Fatalf("text after the error: %+v", c)
				}
			}
		}
	}
	for name, build := range native {
		t.Run(name, func(t *testing.T) {
			assertEndsWithNameError(t, r81integStream(t, r81integSSE(nameless, named, later), build))
		})
	}
	for name, build := range compat {
		t.Run(name+"/index-less", func(t *testing.T) {
			sse := r81integSSE(`{"tool_calls":[{"id":"call_1","type":"function","function":{"arguments":"{}"}}]}`, later)
			assertEndsWithNameError(t, r81integStream(t, sse, build))
		})
		t.Run(name+"/indexed name arrives later", func(t *testing.T) {
			chunks := r81integStream(t, r81integSSE(nameless, named), build)
			var call *types.ToolCall
			finished := false
			for _, c := range chunks {
				switch c.Type {
				case provider.ChunkTypeError:
					t.Fatalf("error chunk: %+v", c)
				case provider.ChunkTypeToolCall:
					call = c.ToolCall
				case provider.ChunkTypeFinish:
					finished = true
				}
			}
			if call == nil || call.ToolName != "t" || call.RawArguments != `{"a":1}` || !finished {
				t.Fatalf("tool call %+v finished %v, want t {\"a\":1} and a finish", call, finished)
			}
		})
	}
}
