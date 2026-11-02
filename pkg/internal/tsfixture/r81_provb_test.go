package tsfixture

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// GenerateContentToTS renders content the way TS returns it, so snapshots of
// `const { content } = await model.doGenerate(...)` (aliases recorded by the
// r79 extractor) compare: an empty content is [], a reasoning file wraps its
// bytes as SharedV4FileData, and a tool call drops the Go-only
// thoughtSignature (TS carries it in providerMetadata).
func TestR81provbGenerateContentTSShapes(t *testing.T) {
	if got := jsonValue(GenerateContentToTS(&types.GenerateResult{})); Diff("", []interface{}{}, got) != "" {
		t.Fatalf("empty content = %#v, want []", got)
	}
	got := GenerateContentToTS(&types.GenerateResult{Content: []types.ContentPart{
		types.ReasoningFileContent{MediaType: "image/png", Data: []byte("hi"), ProviderMetadata: json.RawMessage(`{"google":{"thoughtSignature":"s"}}`)},
		types.ToolCallContent{ToolCallID: "c", ToolName: "t", Input: `{"a":1}`, ThoughtSignature: "s", ProviderMetadata: json.RawMessage(`{"google":{"thoughtSignature":"s"}}`)},
	}})
	want := []interface{}{
		map[string]interface{}{"type": "reasoning-file", "mediaType": "image/png", "data": map[string]interface{}{"type": "data", "data": "aGk="}, "providerMetadata": map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "s"}}},
		map[string]interface{}{"type": "tool-call", "toolCallId": "c", "toolName": "t", "input": `{"a":1}`, "providerMetadata": map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "s"}}},
	}
	if d := Diff("", Normalize(want), Normalize(jsonValue(got))); d != "" {
		t.Fatalf("content (- want, + got):\n%s", d)
	}
}

// A mock fetch response carries only the headers it was recorded with: the
// replay server neither adds the test server's defaults nor lets Go sniff a
// content type (fireworks "should use custom fetch function when provided":
// TS response headers {}).
func TestR81provbMockFetchResponseHeadersExact(t *testing.T) {
	srv := NewWireServer(WireTest{HTTP: []WireHTTP{
		{Method: "POST", URL: "https://api.example.com/img", MockFetch: true, Response: &WireResponse{Type: "binary", Headers: map[string]string{}, BodyB64: "aGk="}},
		{Method: "POST", URL: "https://api.example.com/json", MockFetch: true, Response: &WireResponse{Type: "json-value", Headers: map[string]string{"x-a": "1"}, Body: map[string]interface{}{"a": 1.0}}},
		{Method: "POST", URL: "https://api.example.com/server", Response: &WireResponse{Type: "binary", BodyB64: "aGk="}},
	}})
	defer srv.Close()
	for path, wantCT := range map[string]string{"/img": "", "/json": "", "/server": "application/octet-stream"} {
		resp, err := http.Post(srv.URL+path, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if got := resp.Header.Get("Content-Type"); got != wantCT {
			t.Errorf("%s: Content-Type = %q, want %q", path, got, wantCT)
		}
	}
}

// The wire comparison ignores the Go-only providerOptions replay field on
// output parts and treats an empty tool input like {} (the empty TS input is pruned).
func TestR81provbWireNormalizeGoOnlyPartFields(t *testing.T) {
	got := wireNormalizeParts([]interface{}{
		map[string]interface{}{"type": "text", "text": "a", "providerOptions": map[string]interface{}{"openai": map[string]interface{}{"itemId": "m"}}},
		map[string]interface{}{"type": "tool-call", "toolName": "t", "input": "{}"},
	})
	want := []interface{}{
		map[string]interface{}{"type": "text", "text": "a"},
		map[string]interface{}{"type": "tool-call", "toolName": "t"},
	}
	if d := DiffValues(want, got); d != "" {
		t.Fatalf("(- want, + got):\n%s", d)
	}
}

// A call the recorder captured no result for (no result, no error) is not
// compared: the openai "phase" tests read the result through a helper and
// record none, which made every Go result a difference.
func TestR81provbWireMissingResultNotCompared(t *testing.T) {
	c := WireCall{Method: "doGenerate"}
	if d := CompareWireResult(nil, WireSuite{LanguageResults: true}, c, map[string]interface{}{"content": []interface{}{}}, nil); d != nil {
		t.Fatalf("diffs = %v", d)
	}
}

// Recordings made since round 81 carry the stream parts the TS test read;
// the replay compares them with the stream conventions of the case replay
// (TS mock / default generateId ids, canonical text deltas), and only when the
// test read the whole stream or the stream failed.
func TestR81provbWireStreamCompared(t *testing.T) {
	ts := []interface{}{
		map[string]interface{}{"type": "stream-start", "warnings": []interface{}{}},
		map[string]interface{}{"type": "reasoning-start", "id": "test-reasoning-id"},
		map[string]interface{}{"type": "reasoning-delta", "id": "test-reasoning-id", "delta": "r"},
		map[string]interface{}{"type": "reasoning-end", "id": "test-reasoning-id"},
		map[string]interface{}{"type": "tool-call", "toolCallId": "1ZjReqfZKp5MOeFU", "toolName": "t", "input": "{}"},
		map[string]interface{}{"type": "finish", "finishReason": map[string]interface{}{"unified": "stop"}},
	}
	goParts := []interface{}{
		map[string]interface{}{"type": "reasoning-start", "id": "reasoning-0"},
		map[string]interface{}{"type": "reasoning-delta", "id": "reasoning-0", "delta": "r"},
		map[string]interface{}{"type": "reasoning-end", "id": "reasoning-0"},
		map[string]interface{}{"type": "tool-call", "toolCallId": "call_0123456789abcdef", "toolName": "t", "input": "{}"},
		map[string]interface{}{"type": "finish", "finishReason": map[string]interface{}{"unified": "stop"}},
	}
	c := WireCall{Method: "doStream", Parts: ts, StreamDone: true}
	if d := compareWireStream(c, map[string]interface{}{"stream": goParts}); d != nil {
		t.Fatalf("diffs = %v", d)
	}
	// a missing finish is a difference
	if d := compareWireStream(c, map[string]interface{}{"stream": goParts[:4]}); d == nil {
		t.Fatal("missing finish not reported")
	}
	// a failed TS stream ends with an error part
	failed := WireCall{Method: "doStream", Parts: ts[:1], StreamError: map[string]interface{}{"$error": map[string]interface{}{"message": "x"}}}
	if d := compareWireStream(failed, map[string]interface{}{"stream": []interface{}{map[string]interface{}{"type": "error", "error": "x"}}}); d != nil {
		t.Fatalf("failed stream diffs = %v", d)
	}
	// a partial read is not compared
	if d := compareWireStream(WireCall{Method: "doStream", Parts: ts[:2]}, map[string]interface{}{"stream": goParts}); d != nil {
		t.Fatalf("partial read compared: %v", d)
	}
}
