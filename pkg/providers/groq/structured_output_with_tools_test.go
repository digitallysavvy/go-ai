package groq

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/groq/src/structured-output-with-tools.test.ts (TS #21989).

var dateSchema = map[string]interface{}{
	"type":       "object",
	"properties": map[string]interface{}{"date": map[string]interface{}{"type": "string"}},
}

func structuredWithToolsOpts(choice types.ToolChoice) *provider.GenerateOptions {
	return &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: dateSchema, Description: "A date"},
		Tools:          []types.Tool{{Name: "resolveDate", Description: "d", Parameters: map[string]interface{}{"type": "object"}}},
		ToolChoice:     choice,
	}
}

func TestGroqJSONResponseToolRequestBody(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "openai/gpt-oss-120b")
	for _, tc := range []struct {
		name   string
		choice types.ToolChoice
		want   interface{}
	}{
		{"auto becomes required", types.ToolChoice{Type: types.ToolChoiceAuto}, "required"},
		{"unset becomes required", types.ToolChoice{}, "required"},
		{"none forces the response tool", types.ToolChoice{Type: types.ToolChoiceNone}, map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "json"}}},
		{"specific tool is kept", types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "resolveDate"}, map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "resolveDate"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, warnings := m.buildRequestBodyWithWarnings(structuredWithToolsOpts(tc.choice), false)
			if _, ok := body["response_format"]; ok {
				t.Fatalf("response_format must be omitted: %#v", body["response_format"])
			}
			if body["parallel_tool_calls"] != false {
				t.Fatalf("parallel_tool_calls = %v", body["parallel_tool_calls"])
			}
			tools := body["tools"].([]map[string]interface{})
			last := tools[len(tools)-1]["function"].(map[string]interface{})
			if len(tools) != 2 || last["name"] != "json" || last["description"] != "A date" {
				t.Fatalf("tools = %#v", tools)
			}
			gotJSON, _ := json.Marshal(body["tool_choice"])
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("tool_choice = %s, want %s", gotJSON, wantJSON)
			}
			for _, w := range warnings {
				if w.Feature == "responseFormat" {
					t.Fatalf("unexpected warning %#v", w)
				}
			}
		})
	}
}

func TestGroqJSONResponseToolNameAvoidsCollisions(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "openai/gpt-oss-120b")
	opts := structuredWithToolsOpts(types.ToolChoice{})
	opts.Tools = append(opts.Tools, types.Tool{Name: "json"}, types.Tool{Name: "json_1"})
	if got := m.jsonResponseToolName(opts); got != "json_2" {
		t.Fatalf("name = %q, want json_2", got)
	}
}

func TestGroqNoJSONResponseToolWithoutFunctionTools(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "openai/gpt-oss-120b")
	opts := structuredWithToolsOpts(types.ToolChoice{})
	opts.Tools = nil
	body, _ := m.buildRequestBodyWithWarnings(opts, false)
	if _, ok := body["response_format"]; !ok {
		t.Fatal("native response_format expected without tools")
	}
}

func TestGroqJSONResponseToolDoGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"chatty","tool_calls":[{"id":"call_1","type":"function","function":{"name":"json","arguments":"{ \"date\":\"2031-06-17\" }"}}]}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "openai/gpt-oss-120b")
	res, err := m.DoGenerate(context.Background(), structuredWithToolsOpts(types.ToolChoice{}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{ "date":"2031-06-17" }` || len(res.ToolCalls) != 0 || res.FinishReason != types.FinishReasonStop || res.RawFinishReason != "tool_calls" {
		t.Fatalf("result = %#v", res)
	}
	var text int
	for _, p := range res.Content {
		if tc, ok := p.(types.TextContent); ok {
			text++
			if tc.Text != `{ "date":"2031-06-17" }` {
				t.Fatalf("text content = %q", tc.Text)
			}
		}
	}
	if text != 1 {
		t.Fatalf("text parts = %d", text)
	}
}

func TestGroqJSONResponseToolDoGenerateKeepsApplicationCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"resolveDate","arguments":"{}"}}]}}],"usage":{}}`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "openai/gpt-oss-120b")
	res, err := m.DoGenerate(context.Background(), structuredWithToolsOpts(types.ToolChoice{}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "" || len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "resolveDate" || res.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("result = %#v", res)
	}
}

func TestGroqJSONResponseToolDoStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"json","arguments":"{\"date\":"}}]},"finish_reason":null}]}

data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"2031-06-17\"}"}}]},"finish_reason":null}]}

data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}

data: [DONE]

`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "openai/gpt-oss-120b")
	stream, err := m.DoStream(context.Background(), structuredWithToolsOpts(types.ToolChoice{}))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	text := ""
	var finish types.FinishReason
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeToolCall, provider.ChunkTypeToolInputStart:
			t.Fatalf("unexpected tool chunk %#v", c)
		case provider.ChunkTypeFinish:
			finish = c.FinishReason
		}
	}
	if text != `{"date":"2031-06-17"}` || finish != types.FinishReasonStop {
		t.Fatalf("text=%q finish=%q", text, finish)
	}
}

// TS pushes the response tool's `arguments` as is: an empty string stays an
// empty text part and still counts as text for the finish reason.
func TestGroqJSONResponseToolEmptyArgumentsKeptEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"json","arguments":""}}]}}],"usage":{}}`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "openai/gpt-oss-120b")
	res, err := m.DoGenerate(context.Background(), structuredWithToolsOpts(types.ToolChoice{}))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, p := range res.Content {
		if tc, ok := p.(types.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	if len(texts) != 1 || texts[0] != "" || res.FinishReason != types.FinishReasonStop {
		t.Fatalf("texts=%q finish=%q", texts, res.FinishReason)
	}
}

func TestGroqJSONResponseToolStreamEmptyArgumentsKeptEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"json","arguments":""}}]},"finish_reason":null}]}

data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "openai/gpt-oss-120b")
	stream, err := m.DoStream(context.Background(), structuredWithToolsOpts(types.ToolChoice{}))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	text, sawText := "", false
	var finish types.FinishReason
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
			sawText = true
		case provider.ChunkTypeFinish:
			finish = c.FinishReason
		}
	}
	if !sawText || text != "" || finish != types.FinishReasonStop {
		t.Fatalf("sawText=%v text=%q finish=%q", sawText, text, finish)
	}
}
