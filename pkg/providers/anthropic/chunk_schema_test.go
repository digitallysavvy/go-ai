package anthropic

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS validates every streaming frame against anthropicChunkSchema (zod); a
// frame of valid JSON with the wrong shape becomes an `error` part
// (TypeValidationError) and the stream keeps reading.
func TestStreamSchemaMismatchIsErrorChunkAndStreamContinues(t *testing.T) {
	cases := map[string]string{
		"message_start missing usage":           `{"type":"message_start","message":{"id":"m"}}`,
		"message_start usage input_tokens str":  `{"type":"message_start","message":{"usage":{"input_tokens":"3"}}}`,
		"message_start content not tool_use":    `{"type":"message_start","message":{"usage":{"input_tokens":1},"content":[{"type":"text","text":"x"}]}}`,
		"content_block_start index string":      `{"type":"content_block_start","index":"0","content_block":{"type":"text","text":""}}`,
		"content_block_start unknown block":     `{"type":"content_block_start","index":0,"content_block":{"type":"nope"}}`,
		"content_block_start text no text":      `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		"content_block_start tool_use null in":  `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"a","name":"b","input":null}}`,
		"content_block_start web_fetch bad":     `{"type":"content_block_start","index":0,"content_block":{"type":"web_fetch_tool_result","tool_use_id":"a","content":{"type":"web_fetch_result","url":"u"}}}`,
		"content_block_start mcp result object": `{"type":"content_block_start","index":0,"content_block":{"type":"mcp_tool_result","tool_use_id":"a","is_error":false,"content":{"results":[]}}}`,
		"content_block_delta unknown delta":     `{"type":"content_block_delta","index":0,"delta":{"type":"nope"}}`,
		"content_block_delta text not string":   `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":5}}`,
		"content_block_delta no delta":          `{"type":"content_block_delta","index":0}`,
		"content_block_delta bad citation":      `{"type":"content_block_delta","index":0,"delta":{"type":"citations_delta","citation":{"type":"char_location"}}}`,
		"content_block_stop no index":           `{"type":"content_block_stop"}`,
		"error no message":                      `{"type":"error","error":{"type":"overloaded_error"}}`,
		"error not object":                      `{"type":"error","error":"boom"}`,
		"message_delta no usage":                `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
		"message_delta output_tokens string":    `{"type":"message_delta","delta":{},"usage":{"output_tokens":"2"}}`,
		"message_delta bad container":           `{"type":"message_delta","delta":{"container":{"id":"c"}},"usage":{"output_tokens":2}}`,
		"message_delta bad iterations type":     `{"type":"message_delta","delta":{},"usage":{"output_tokens":2,"iterations":[{"type":"x","input_tokens":1,"output_tokens":1}]}}`,
		"ping wrong type literal":               `{"type":"pong"}`,
		"message_stop wrong type literal":       `{"type":"message_stopped"}`,
		"missing type":                          `{"index":0}`,
		"not an object":                         `[1,2]`,
	}
	for name, frame := range cases {
		t.Run(name, func(t *testing.T) {
			sse := "event: message_start\ndata: " + frame + "\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
			stream := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), false)
			chunk, err := stream.Next()
			if err != nil {
				t.Fatalf("Next() error: %v", err)
			}
			if chunk.Type != provider.ChunkTypeError || chunk.Err == nil {
				t.Fatalf("first chunk = %+v, want error chunk", chunk)
			}
			if !strings.HasPrefix(chunk.Text, "Type validation failed: Value: ") || !strings.Contains(chunk.Text, ".\nError message: ") {
				t.Errorf("message = %q", chunk.Text)
			}
			var verr *providererrors.ValidationError
			if !errors.As(chunk.Err, &verr) {
				t.Errorf("Err = %T, want *ValidationError", chunk.Err)
			}
			chunk, err = stream.Next()
			if err != nil {
				t.Fatalf("Next() after error chunk: %v", err)
			}
			if chunk.Type != provider.ChunkTypeText || chunk.Text != "Hello" {
				t.Fatalf("second chunk = %+v, want text Hello", chunk)
			}
		})
	}
}

// Frames the zod schema accepts (extra keys, nullish fields, optional
// defaults) must not be rejected.
func TestChunkSchemaAcceptsValidFrames(t *testing.T) {
	frames := []string{
		`{"type":"ping"}`,
		`{"type":"message_stop","extra":1}`,
		`{"type":"message_start","message":{"id":null,"usage":{"input_tokens":1,"service_tier":"x"},"content":[{"type":"tool_use","id":"a","name":"n","input":{"a":1}}],"container":{"id":"c","expires_at":"e"}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"","citations":[]}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"a","name":"n","input":{},"caller":{"type":"direct"},"toolset_name":null}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"a","name":"n","input":null}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"mcp_tool_result","tool_use_id":"a","is_error":false,"content":[{"type":"text","text":"x","citations":null},"s"]}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"code_execution_tool_result","tool_use_id":"a","content":{"type":"code_execution_result","stdout":"","stderr":"","return_code":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"web_search_tool_result","tool_use_id":"a","content":[{"type":"web_search_result","url":"u","title":null,"encrypted_content":"e"}]}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"fallback","from":{"model":"a"},"to":{"model":"b"}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","cited_text":"c","url":"u","title":null,"encrypted_index":"i"}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"compaction_delta","content":null}}`,
		`{"type":"error","error":{"type":"overloaded_error","message":"m","code":529}}`,
		`{"type":"message_delta","delta":{"stop_reason":null,"container":{"id":"c","expires_at":"e","skills":[{"type":"custom","skill_id":"s","version":"1"}]}},"usage":{"output_tokens":2,"iterations":[{"type":"message","input_tokens":1,"output_tokens":1}]},"context_management":{"applied_edits":[{"type":"compact_20260112"}]}}`,
	}
	for _, f := range frames {
		if msg := anthropicChunkValidationMessage(f); msg != "" {
			t.Errorf("frame rejected: %s\n%s", f, msg)
		}
	}
}
