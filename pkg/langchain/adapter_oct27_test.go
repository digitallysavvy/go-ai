package langchain

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23690: large inline binary content is base64-encoded without a stack
// overflow. Go's encoder has no argument-count limit; this pins the exact
// output for payloads above the JS spread limit.
func TestConvertUserContentLargeBinary(t *testing.T) {
	data := bytes.Repeat([]byte{0, 1, 2, 250, 251, 255}, 50_000) // 300 KB
	want := base64.StdEncoding.EncodeToString(data)

	for name, part := range map[string]types.ContentPart{
		"image": types.ImageContent{Image: data, MimeType: "image/png"},
		"file":  types.FileContent{Data: data, MediaType: "application/pdf", Filename: "large.pdf"},
	} {
		t.Run(name, func(t *testing.T) {
			blocks, ok := convertUserContent([]types.ContentPart{part}).([]interface{})
			if !ok || len(blocks) != 1 {
				t.Fatalf("blocks = %#v", blocks)
			}
			block := blocks[0].(map[string]interface{})
			if block["data"] != want {
				t.Fatalf("data mismatch (len %d)", len(block["data"].(string)))
			}
		})
	}
}

// TS #16320: OpenAI-format tool call arguments are untrusted model output and
// parse with secureJsonParse; unsafe or invalid JSON falls back to {}.
func TestToolCallArgumentsUseSecureJSONParse(t *testing.T) {
	for _, args := range []string{
		`{"__proto__":{"polluted":true}}`,
		`{"constructor":{"prototype":{"polluted":true}}}`,
		`{"nested":[{"__proto__":{}}]}`,
		`invalid JSON`,
	} {
		t.Run(args, func(t *testing.T) {
			events := make(chan StreamEvent, 1)
			events <- StreamEvent{Mode: "values", Data: map[string]interface{}{"messages": []interface{}{
				map[string]interface{}{"id": "msg-1", "type": "ai", "content": "", "additional_kwargs": map[string]interface{}{
					"tool_calls": []interface{}{map[string]interface{}{
						"id": "call-1", "function": map[string]interface{}{"name": "get_weather", "arguments": args},
					}},
				}},
			}}}
			close(events)
			out, errCh := ToUIMessageStream(context.Background(), events)
			var input interface{}
			found := false
			for c := range out {
				if c["type"] == "tool-input-available" {
					input, found = c["input"], true
				}
			}
			for range errCh {
			}
			m, ok := input.(map[string]interface{})
			if !found || !ok || len(m) != 0 {
				t.Fatalf("found=%v input=%#v, want {}", found, input)
			}
		})
	}

	// Safe arguments (including a benign "constructor" key) still parse.
	got := parseArgs(`{"city":"Paris","constructor":"x"}`)
	if m, ok := got.(map[string]interface{}); !ok || m["city"] != "Paris" {
		t.Fatalf("safe args = %#v", got)
	}
}
