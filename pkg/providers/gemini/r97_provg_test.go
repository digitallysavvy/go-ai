package gemini

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convertGenerateContentResponse: a functionCall tool call's input is
// JSON.stringify(args ?? {}), so a string args value becomes a quoted JSON
// string (the stream uses it verbatim; that path is unchanged).
func TestR97provgGenerateStringArgsToolCallInputIsStringified(t *testing.T) {
	body := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"c1","name":"t","args":"a\"b"}}]},"finishReason":"STOP"}]}`
	var resp Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	m := NewLanguageModel(Config{MetadataKey: "google"}, "gemini-2.5-flash")
	result := m.convertResponse(resp, newToolNameMapping(nil))
	if len(result.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(result.ToolCalls))
	}
	if got, want := result.ToolCalls[0].RawArguments, `"a\"b"`; got != want {
		t.Fatalf("RawArguments = %s, want %s", got, want)
	}
	tc, ok := result.Content[0].(types.ToolCallContent)
	if !ok || tc.Input != `"a\"b"` {
		t.Fatalf("content = %#v", result.Content[0])
	}

	// An object args value keeps its wire text.
	body = `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"c1","name":"t","args":{"b":1,"a":2}}}]},"finishReason":"STOP"}]}`
	resp = Response{}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	if got := m.convertResponse(resp, newToolNameMapping(nil)).ToolCalls[0].RawArguments; got != `{"b":1,"a":2}` {
		t.Fatalf("RawArguments = %s", got)
	}
}
