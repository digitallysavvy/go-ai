package providers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

// Same rule as TestChatCompletionsToolCallsKeepRawArguments for the
// Responses-style and other wire formats (non-streaming).
func TestOtherProtocolsToolCallsKeepRawArguments(t *testing.T) {
	const args = `{"zeta":1,"alpha":{"yy":1,"bb":2}}`
	const escaped = `{\"zeta\":1,\"alpha\":{\"yy\":1,\"bb\":2}}`
	responsesBody := `{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"m","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"t","arguments":"` + escaped + `","status":"completed"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	cohereBody := `{"id":"c","finish_reason":"TOOL_CALL","message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"t","arguments":"` + escaped + `"}}]},"usage":{"tokens":{"input_tokens":1,"output_tokens":1}}}`
	ollamaBody := `{"id":"c","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"t","arguments":"` + escaped + `"}}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	lm := func(m provider.LanguageModel, err error) provider.LanguageModel {
		if err != nil {
			panic(err)
		}
		return m
	}
	cases := []struct {
		name string
		body string
		mk   func(u string) provider.LanguageModel
	}{
		{"openai-responses", responsesBody, func(u string) provider.LanguageModel {
			return lm(openai.New(openai.Config{APIKey: "k", BaseURL: u}).ResponsesModel("gpt-4o"))
		}},
		{"openresponses", responsesBody, func(u string) provider.LanguageModel {
			return lm(openresponses.New(openresponses.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		}},
		{"xai-responses", responsesBody, func(u string) provider.LanguageModel {
			return lm(xai.New(xai.Config{APIKey: "k", BaseURL: u}).LanguageModel("grok-4"))
		}},
		{"huggingface", responsesBody, func(u string) provider.LanguageModel {
			return lm(huggingface.New(huggingface.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		}},
		{"cohere", cohereBody, func(u string) provider.LanguageModel {
			return lm(cohere.New(cohere.Config{APIKey: "k", BaseURL: u}).LanguageModel("command-r"))
		}},
		{"ollama", ollamaBody, func(u string) provider.LanguageModel {
			return lm(ollama.New(ollama.Config{BaseURL: u}).LanguageModel("llama3"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			res, err := tc.mk(srv.URL).DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "hi"},
				Tools:  []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}},
			})
			if err != nil || len(res.ToolCalls) != 1 {
				t.Fatalf("generate: err=%v calls=%d", err, len(res.ToolCalls))
			}
			if got := res.ToolCalls[0].RawArguments; got != args {
				t.Errorf("RawArguments = %q, want %q", got, args)
			}
		})
	}
}

// Streaming counterpart for the Responses-style wire formats and Cohere.
func TestOtherProtocolsStreamedToolCallsKeepRawArguments(t *testing.T) {
	const args = `{"zeta":1,"alpha":{"yy":1,"bb":2}}`
	const escaped = `{\"zeta\":1,\"alpha\":{\"yy\":1,\"bb\":2}}`
	sse := func(events ...string) string {
		out := ""
		for _, e := range events {
			out += "data: " + e + "\n\n"
		}
		return out + "data: [DONE]\n\n"
	}
	responsesStream := sse(
		`{"type":"response.created","response":{"id":"resp_1","object":"response","created_at":1,"status":"in_progress","model":"m","output":[]}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"t","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"`+escaped+`"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"arguments":"`+escaped+`"}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"t","arguments":"`+escaped+`","status":"completed"}}`,
		`{"type":"response.completed","response":{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"m","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"t","arguments":"`+escaped+`","status":"completed"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	)
	cohereStream := "event: stream-start\ndata: {\"type\":\"message-start\",\"id\":\"c\"}\n\n" +
		"data: {\"type\":\"tool-call-start\",\"index\":0,\"delta\":{\"message\":{\"tool_calls\":{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"t\",\"arguments\":\"\"}}}}}\n\n" +
		"data: {\"type\":\"tool-call-delta\",\"index\":0,\"delta\":{\"message\":{\"tool_calls\":{\"function\":{\"arguments\":\"" + escaped + "\"}}}}}\n\n" +
		"data: {\"type\":\"tool-call-end\",\"index\":0}\n\n" +
		"data: {\"type\":\"message-end\",\"delta\":{\"finish_reason\":\"TOOL_CALL\",\"usage\":{\"tokens\":{\"input_tokens\":1,\"output_tokens\":1}}}}\n\n"
	lm := func(m provider.LanguageModel, err error) provider.LanguageModel {
		if err != nil {
			panic(err)
		}
		return m
	}
	cases := []struct {
		name string
		body string
		mk   func(u string) provider.LanguageModel
	}{
		{"openai-responses", responsesStream, func(u string) provider.LanguageModel {
			return lm(openai.New(openai.Config{APIKey: "k", BaseURL: u}).ResponsesModel("gpt-4o"))
		}},
		{"openresponses", responsesStream, func(u string) provider.LanguageModel {
			return lm(openresponses.New(openresponses.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		}},
		{"xai-responses", responsesStream, func(u string) provider.LanguageModel {
			return lm(xai.New(xai.Config{APIKey: "k", BaseURL: u}).LanguageModel("grok-4"))
		}},
		{"huggingface", responsesStream, func(u string) provider.LanguageModel {
			return lm(huggingface.New(huggingface.Config{APIKey: "k", BaseURL: u}).LanguageModel("m"))
		}},
		{"cohere", cohereStream, func(u string) provider.LanguageModel {
			return lm(cohere.New(cohere.Config{APIKey: "k", BaseURL: u}).LanguageModel("command-r"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			stream, err := tc.mk(srv.URL).DoStream(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{Text: "hi"},
				Tools:  []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"}}},
			})
			if err != nil {
				t.Fatal(err)
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
				t.Fatal("no tool call chunk")
			}
			if got.RawArguments != args {
				t.Errorf("RawArguments = %q, want %q", got.RawArguments, args)
			}
		})
	}
}
