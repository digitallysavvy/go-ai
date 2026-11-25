package gemini

import (
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getContentSchema is a zod union whose members strip the keys they do not
// declare: a functionCall part that also has text, or a toolCall part with
// text, is only the call; a toolCall/toolResponse without a string id or an
// inlineData without mimeType/data falls to the last member and is dropped.
func TestConvertResponse_PartUnionMembers(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	body := `{"candidates":[{"content":{"parts":[
		{"functionCall":{"id":"c1","name":"f","args":{"a":1}},"text":"dropped-fc"},
		{"toolCall":{"toolType":"GOOGLE_SEARCH","id":"s1","args":{}},"text":"dropped-tc"},
		{"toolCall":{"toolType":"GOOGLE_SEARCH"}},
		{"toolResponse":{"toolType":"GOOGLE_SEARCH","response":{}}},
		{"inlineData":{"mimeType":"image/png"}},
		{"text":"kept"}
	]},"finishReason":"STOP"}]}`
	var resp Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	result := m.convertResponse(resp, nil)
	if result.Text != "kept" {
		t.Errorf("Text = %q, want %q", result.Text, "kept")
	}
	var kinds []string
	for _, c := range result.Content {
		switch v := c.(type) {
		case types.TextContent:
			kinds = append(kinds, "text:"+v.Text)
		case types.ToolCallContent:
			kinds = append(kinds, "call:"+v.ToolCallID)
		case types.ToolResultContent:
			kinds = append(kinds, "result:"+v.ToolCallID)
		case types.GeneratedFileContent:
			kinds = append(kinds, "file")
		default:
			kinds = append(kinds, "other")
		}
	}
	want := []string{"call:c1", "call:s1", "text:kept"}
	if len(kinds) != len(want) {
		t.Fatalf("content = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("content = %v, want %v", kinds, want)
		}
	}
}

// The stream already skipped text on functionCall parts; the toolCall member
// now needs its id there too.
func TestStream_PartUnionDropsToolCallWithoutID(t *testing.T) {
	s := newTestStream(sseStream(`{"candidates":[{"content":{"parts":[{"toolCall":{"toolType":"GOOGLE_SEARCH"}},{"text":"hi","thoughtSignature":"sig"}]},"finishReason":"STOP"}]}`))
	for _, c := range drainChunks(t, s) {
		if c.Type == provider.ChunkTypeToolCall {
			t.Fatalf("tool call emitted for a toolCall without id: %+v", c.ToolCall)
		}
	}
}

// TS continueGoogleStream enqueues a failed continuation as a parse result
// with rawValue undefined, so includeRawChunks puts a raw part before the
// error part.
func TestStreamContinuationFailureRawChunk(t *testing.T) {
	s := newTestStream(sseStream(`{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"CONTINUATION","continuationToken":"tok"}]}`))
	s.includeRawChunks = true
	s.continueFn = func(string) (io.ReadCloser, error) { return nil, errors.New("down") }
	chunks := drainChunks(t, s)
	for i, c := range chunks {
		if c.Type != provider.ChunkTypeError {
			continue
		}
		if i == 0 || chunks[i-1].Type != provider.ChunkTypeRaw || chunks[i-1].Raw != nil {
			t.Fatalf("continuation error not preceded by an empty raw chunk: %v", chunkTypes(chunks))
		}
		return
	}
	t.Fatalf("no error chunk: %v", chunkTypes(chunks))
}
