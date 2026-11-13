package gemini

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertGenerateContentResponse dispatches on the part's data field
// ('executableCode' / 'text' / 'functionCall' / 'toolCall' in part), so a
// part flagged thought: true without a text key still becomes a tool call or
// code execution call. DoGenerate dropped these parts; the stream kept them.
func TestConvertResponse_ThoughtFlaggedNonTextPartsAreKept(t *testing.T) {
	m := makeTestModel("gemini-3-pro-preview")
	var resp Response
	if err := json.Unmarshal([]byte(`{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[
		{"thought":true,"text":"thinking"},
		{"thought":true,"thoughtSignature":"sig-fc","functionCall":{"id":"c1","name":"get_weather","args":{"city":"Paris"}}},
		{"thought":true,"executableCode":{"language":"PYTHON","code":"print(1)"}},
		{"thought":true,"toolCall":{"id":"s1","toolType":"GOOGLE_SEARCH","args":{"q":"x"}}}
	]}}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	result := m.convertResponseFull(resp, toolNameMapping{}, "", nil)
	if len(result.Content) != 4 {
		t.Fatalf("content = %#v, want reasoning + 3 tool calls", result.Content)
	}
	if r, ok := result.Content[0].(types.ReasoningContent); !ok || r.Text != "thinking" {
		t.Fatalf("content[0] = %#v", result.Content[0])
	}
	call, ok := result.Content[1].(types.ToolCallContent)
	if !ok || call.ToolCallID != "c1" || call.ToolName != "get_weather" || call.ThoughtSignature != "sig-fc" {
		t.Fatalf("content[1] = %#v", result.Content[1])
	}
	if c, ok := result.Content[2].(types.ToolCallContent); !ok || c.ToolName != "code_execution" || !c.ProviderExecuted {
		t.Fatalf("content[2] = %#v", result.Content[2])
	}
	if c, ok := result.Content[3].(types.ToolCallContent); !ok || c.ToolName != "server:GOOGLE_SEARCH" {
		t.Fatalf("content[3] = %#v", result.Content[3])
	}
	if result.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish reason = %q, want tool-calls", result.FinishReason)
	}
}
