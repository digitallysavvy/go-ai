package gemini

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS checks `functionCall.name != null`, so an empty-string name still
// names a call (generate).
func TestR56ConvertResponseEmptyFunctionCallNameIsToolCall(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	var resp Response
	if err := json.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"c1","name":"","args":{"a":1}}}]},"finishReason":"STOP"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	result := m.convertResponse(resp, nil)
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "c1" || result.ToolCalls[0].ToolName != "" {
		t.Fatalf("tool calls = %#v, want one call c1 with an empty name", result.ToolCalls)
	}
	if result.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish reason = %q, want tool-calls", result.FinishReason)
	}

	// A null name is still no call.
	resp = Response{}
	if err := json.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"c1","name":null,"args":{"a":1}}}]},"finishReason":"STOP"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	if result := m.convertResponse(resp, nil); len(result.ToolCalls) != 0 {
		t.Fatalf("null name: tool calls = %#v, want none", result.ToolCalls)
	}
}

// Stream: an empty-string name with args is a complete call, and with no
// args a no-args complete call, not a terminal chunk.
func TestR56StreamEmptyFunctionCallNameIsToolCall(t *testing.T) {
	for name, part := range map[string]string{
		"complete": `{"functionCall":{"id":"c1","name":"","args":{"a":1}}}`,
		"no-args":  `{"functionCall":{"id":"c1","name":""}}`,
	} {
		body := `data: {"candidates":[{"content":{"parts":[` + part + `]},"finishReason":"STOP"}]}` + "\n\n"
		s := newTestStream(io.NopCloser(strings.NewReader(body)))
		var call *types.ToolCall
		var finish types.FinishReason
		for _, c := range drainStream(t, s) {
			switch c.Type {
			case provider.ChunkTypeToolCall:
				call = c.ToolCall
			case provider.ChunkTypeFinish:
				finish = c.FinishReason
			}
		}
		if call == nil || call.ID != "c1" || call.ToolName != "" {
			t.Fatalf("%s: tool call = %#v, want c1 with an empty name", name, call)
		}
		if finish != types.FinishReasonToolCalls {
			t.Fatalf("%s: finish = %q, want tool-calls", name, finish)
		}
	}
}

// r56: TS checks executableCode and codeExecutionResult before text, so a
// thought part that also carries code is a code execution call/result, not
// reasoning (generate; the stream already does this).
func TestR56ConvertResponseThoughtCodePartIsCodeExecution(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	var resp Response
	if err := json.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[`+
		`{"executableCode":{"language":"PYTHON","code":"print(1)"},"text":"t","thought":true},`+
		`{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1"},"text":"u","thought":true}`+
		`]},"finishReason":"STOP"}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	result := m.convertResponse(resp, newToolNameMapping(nil))
	if len(result.Content) != 2 {
		t.Fatalf("content = %#v, want a code call and its result", result.Content)
	}
	call, ok := result.Content[0].(types.ToolCallContent)
	if !ok || call.ToolName != "code_execution" || !call.ProviderExecuted {
		t.Fatalf("content[0] = %#v, want code_execution call", result.Content[0])
	}
	if r, ok := result.Content[1].(types.ToolResultContent); !ok || r.ToolCallID != call.ToolCallID {
		t.Fatalf("content[1] = %#v, want its result", result.Content[1])
	}
}
