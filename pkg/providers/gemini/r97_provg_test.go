package gemini

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// TS toolResponse.response is z.unknown(): the tool result is
// `response ?? {}`, so a non-object response is the result as is, in
// doGenerate and in the stream.
func TestR97provgToolResponseNonObjectResponseKept(t *testing.T) {
	body := `{"candidates":[{"content":{"parts":[{"toolCall":{"toolType":"T","id":"s1","args":{}}},{"toolResponse":{"toolType":"T","id":"s1","response":"plain"}}]},"finishReason":"STOP"}]}`
	var resp Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	m := NewLanguageModel(Config{MetadataKey: "google"}, "gemini-2.5-flash")
	result := m.convertResponse(resp, newToolNameMapping(nil))
	var got interface{}
	for _, c := range result.Content {
		if tr, ok := c.(types.ToolResultContent); ok {
			got = tr.Result
		}
	}
	if got != "plain" {
		t.Fatalf("generate result = %#v, want \"plain\"", got)
	}

	s := newStream(sseStream(body), Config{MetadataKey: "google"}, newToolNameMapping(nil), nil, "")
	var streamed interface{}
	for i := 0; i < 50; i++ {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolResult {
			streamed = c.ToolResult.Result
		}
	}
	if streamed != "plain" {
		t.Fatalf("stream result = %#v, want \"plain\"", streamed)
	}
}

// TS createProviderStreamError data is the parsed in-stream error: a number
// beyond the double range in its details reads as Infinity (ruling 4), so
// the data keeps the error instead of losing it.
func TestR97provgStreamErrorDataKeepsNonFiniteDetails(t *testing.T) {
	s := newStream(sseStream(`{"error":{"code":500,"message":"boom","status":"INTERNAL","details":[1e400]}}`),
		Config{MetadataKey: "google", ProviderName: "google.generative-ai"}, newToolNameMapping(nil), nil, "")
	for i := 0; i < 50; i++ {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type != provider.ChunkTypeError {
			continue
		}
		var spe *providererrors.StreamProviderError
		if !errors.As(c.Err, &spe) {
			t.Fatalf("err = %T", c.Err)
		}
		data, ok := spe.Data.(map[string]interface{})
		if !ok || data["message"] != "boom" {
			t.Fatalf("data = %#v, want the parsed error", spe.Data)
		}
		details, _ := data["details"].([]interface{})
		if len(details) != 1 {
			t.Fatalf("details = %#v", data["details"])
		}
		if f, ok := details[0].(float64); !ok || !math.IsInf(f, 1) {
			t.Fatalf("details[0] = %#v, want +Inf", details[0])
		}
		return
	}
	t.Fatal("no error part")
}
