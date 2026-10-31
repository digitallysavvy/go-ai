package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from openai-responses-language-model.test.ts "shell tool with
// execution controls" (TS #23441).

func shellControlsOpts() *provider.GenerateOptions {
	return &provider.GenerateOptions{
		Prompt: promptCachePrompt(),
		Tools:  []types.Tool{{Type: types.ToolTypeProviderDefined, Name: "shell", ProviderID: "openai.shell"}},
	}
}

func shellControlsServer(t *testing.T, items []string, stream bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, `data: {"type":"response.created","response":{"id":"r","created_at":1,"model":"gpt-5.1"}}`+"\n\n")
			for i, item := range items {
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.output_item.added\",\"output_index\":%d,\"item\":%s}\n\n", i, item)
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":%s}\n\n", i, item)
			}
			_, _ = fmt.Fprint(w, `data: {"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":1}}}`+"\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"resp","object":"response","created_at":1,"status":"completed","error":null,"incomplete_details":null,"model":"gpt-5.1","output":[%s],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`, strings.Join(items, ","))
	}))
}

func collectShell(t *testing.T, stream bool, items ...string) (calls []types.ToolCall, results []interface{}) {
	t.Helper()
	srv := shellControlsServer(t, items, stream)
	defer srv.Close()
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-5.1")
	if !stream {
		res, err := model.DoGenerate(context.Background(), shellControlsOpts())
		if err != nil {
			t.Fatal(err)
		}
		calls = res.ToolCalls
		for _, p := range res.Content {
			if r, ok := p.(types.ToolResultContent); ok {
				results = append(results, r.Result)
			}
		}
		return calls, results
	}
	s, err := model.DoStream(context.Background(), shellControlsOpts())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.ToolCall != nil && c.Type == provider.ChunkTypeToolCall {
			calls = append(calls, *c.ToolCall)
		}
		if c.ToolResult != nil && c.Type == provider.ChunkTypeToolResult {
			results = append(results, c.ToolResult.Result)
		}
	}
	return calls, results
}

func TestShellCallExecutionControlsForwarded(t *testing.T) {
	const call = `{"id":"sh_1","type":"shell_call","status":"completed","call_id":"call_1","action":{"commands":["echo hi"],%s}}`
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name+"/forwards timeout_ms and max_output_length", func(t *testing.T) {
			calls, _ := collectShell(t, stream, fmt.Sprintf(call, `"timeout_ms":1000,"max_output_length":10000`))
			if len(calls) != 1 || calls[0].RawArguments != `{"action":{"commands":["echo hi"],"timeoutMs":1000,"maxOutputLength":10000}}` {
				t.Fatalf("calls = %#v", calls)
			}
		})
		t.Run(name+"/omits null controls", func(t *testing.T) {
			calls, _ := collectShell(t, stream, fmt.Sprintf(call, `"timeout_ms":null,"max_output_length":null`))
			if len(calls) != 1 || calls[0].RawArguments != `{"action":{"commands":["echo hi"]}}` {
				t.Fatalf("calls = %#v", calls)
			}
		})
	}
}

func TestShellCallOutputMaxOutputLengthOnToolResult(t *testing.T) {
	items := []string{`{"id":"sh_1","type":"shell_call","status":"completed","call_id":"call_1","action":{"commands":["echo hi"],"max_output_length":10000}}`,
		`{"id":"sho_1","type":"shell_call_output","status":"completed","call_id":"call_1","max_output_length":10000,"output":[{"stdout":"hi\n","stderr":"","outcome":{"type":"exit","exit_code":0}}]}`}
	for _, stream := range []bool{false, true} {
		_, results := collectShell(t, stream, items...)
		if len(results) != 1 {
			t.Fatalf("stream=%v results = %#v", stream, results)
		}
		m, _ := results[0].(map[string]interface{})
		if m["maxOutputLength"] != float64(10000) {
			t.Fatalf("stream=%v result = %#v", stream, results[0])
		}
	}
}
