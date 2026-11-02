package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

func r81provbCompletionGenerate(t *testing.T, cfg Config, body string) (*types.GenerateResult, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	cfg.APIKey, cfg.BaseURL = "k", srv.URL
	m, err := New(cfg).CompletionModel("gpt-3.5-turbo-instruct")
	if err != nil {
		t.Fatal(err)
	}
	return m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
}

// TS completion doGenerate reads response.choices[0] without a guard: the
// first property read is a TypeError (openai choice.logprobs,
// openai-compatible choice.text). Go returned an empty result.
func TestR81provbCompletionEmptyChoicesIsError(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"openai", Config{}, "Cannot read properties of undefined (reading 'logprobs')"},
		{"openai-compatible", Config{OpenAICompatible: true, Name: "acme"}, "Cannot read properties of undefined (reading 'text')"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := r81provbCompletionGenerate(t, tc.cfg, `{"id":"c","object":"text_completion","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v, result = %+v", err, res)
			}
		})
	}
}

// TS openai-compatible completion pushes the text part only when the text is
// non-empty; TS openai always pushes it.
func TestR81provbCompletionEmptyTextContent(t *testing.T) {
	body := `{"id":"c","object":"text_completion","created":1,"model":"m","choices":[{"index":0,"text":"","finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`
	res, err := r81provbCompletionGenerate(t, Config{OpenAICompatible: true, Name: "acme"}, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 0 {
		t.Fatalf("openai-compatible content = %#v, want none", res.Content)
	}
	res, err = r81provbCompletionGenerate(t, Config{}, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("openai content = %#v, want one empty text part", res.Content)
	}
}

// TS mapComputerAction writes the declared keys in its own order, drops a null
// `keys`, and the zod schema strips undeclared keys; the tool input text is
// JSON.stringify of that object (OpenAI Responses computer tool "decodes
// batched actions in non-streaming responses", replayed with results).
func TestR81provbComputerCallInputTSOrder(t *testing.T) {
	var item responses.ComputerCall
	if err := json.Unmarshal([]byte(`{"type":"computer_call","id":"cu_1","call_id":"call_1","status":"completed",
		"actions":[
			{"x":100,"y":200,"button":"left","keys":["CTRL"],"type":"click"},
			{"y":210,"x":110,"keys":null,"type":"double_click"},
			{"scroll_y":-5,"scroll_x":0,"y":2,"x":1,"type":"scroll","extra":true},
			{"keys":["CTRL","L"],"type":"keypress"}
		],
		"pending_safety_checks":[{"message":"m","code":"c","id":"sc_1"}]}`), &item); err != nil {
		t.Fatal(err)
	}
	args, raw := computerCallArguments(item)
	want := `{"actions":[{"type":"click","button":"left","x":100,"y":200,"keys":["CTRL"]},{"type":"double_click","x":110,"y":210},{"type":"scroll","x":1,"y":2,"scrollX":0,"scrollY":-5},{"type":"keypress","keys":["CTRL","L"]}],"pendingSafetyChecks":[{"id":"sc_1","code":"c","message":"m"}],"status":"completed"}`
	if raw != want {
		t.Fatalf("input =\n%s\nwant\n%s", raw, want)
	}
	actions, ok := args["actions"].([]map[string]interface{})
	if !ok || len(actions) != 4 {
		t.Fatalf("actions = %#v", args["actions"])
	}
	if _, has := actions[1]["keys"]; has {
		t.Fatalf("double_click keeps a null keys: %#v", actions[1])
	}
}

// TS openai-compatible extractMetadata: a streamed tool call delta's
// extra_content.google.thought_signature becomes the tool-call part's
// providerMetadata[providerOptionsName] (openai-compatible "should stream
// tool call with thought signature from extra_content", replayed with stream
// parts). Generate already did this; the stream dropped it.
func TestR81provbCompatStreamToolCallThoughtSignature(t *testing.T) {
	sse := `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"fc-1","type":"function","function":{"name":"check","arguments":""},"extra_content":{"google":{"thought_signature":"<Sig>"}}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"
	chunks, _ := collectChat(t, sse, Config{OpenAICompatible: true, Name: "acme"})
	var call *provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			call = c
		}
	}
	if call == nil || call.ToolCall == nil {
		t.Fatalf("no tool call in %v", chunks)
	}
	var got interface{}
	b, _ := json.Marshal(call.ToolCall.ProviderMetadata)
	_ = json.Unmarshal(b, &got)
	want := map[string]interface{}{"acme": map[string]interface{}{"thoughtSignature": "<Sig>"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("providerMetadata = %s, want %v", b, want)
	}
	// native OpenAI chat has no extra_content
	chunks, _ = collectChat(t, sse, Config{})
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall && c.ToolCall != nil && c.ToolCall.ProviderMetadata != nil {
			t.Fatalf("openai tool call metadata = %v", c.ToolCall.ProviderMetadata)
		}
	}
}

// A streamed tool call whose function.name never arrives makes TS
// toolCallTracker.flush() throw, so flush() emits no finish part
// (openai-compatible "should error when streamed tool call never receives a
// function.name"). Go emitted the error and then a finish.
func TestR81provbStreamMissingToolNameNoFinish(t *testing.T) {
	sse := `data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":null},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_missing","type":"function","function":{"arguments":"{}"}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\ndata: [DONE]\n\n"
	for name, cfg := range map[string]Config{"openai-compatible": {OpenAICompatible: true, Name: "acme"}, "openai": {}} {
		t.Run(name, func(t *testing.T) {
			chunks, _ := collectChat(t, sse, cfg)
			sawError := false
			for _, c := range chunks {
				switch c.Type {
				case provider.ChunkTypeError:
					sawError = true
				case provider.ChunkTypeFinish:
					t.Fatalf("finish after the tool call error: %+v", c)
				}
			}
			if !sawError {
				t.Fatalf("no error part in %v", chunks)
			}
		})
	}
}
