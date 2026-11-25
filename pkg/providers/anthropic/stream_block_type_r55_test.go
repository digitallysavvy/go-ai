package anthropic

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r55AnthropicChunks(t *testing.T, sse string, jsonTool bool) []*provider.StreamChunk {
	t.Helper()
	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), jsonTool)
	var out []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err != nil {
			return out
		}
		out = append(out, c)
	}
}

const r55MsgStart = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n"

// TS emits a signature reasoning-delta only while the stream-wide blockType
// (last content_block_start, cleared by content_block_stop) is 'thinking'.
func TestR55StreamSignatureDeltaOnlyWhileLastStartedBlockIsThinking(t *testing.T) {
	for _, tc := range []struct {
		name string
		sse  string
	}{
		{"redacted thinking", "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"redacted_thinking\",\"data\":\"d\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"s\"}}\n\n"},
		{"later text block started", "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"s\"}}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, c := range r55AnthropicChunks(t, r55MsgStart+tc.sse, false) {
				if c.Type == provider.ChunkTypeReasoning && c.ProviderMetadata != nil {
					t.Fatalf("unexpected signature reasoning delta: %s", c.ProviderMetadata)
				}
			}
		})
	}
}

// TS: once the json response tool started, input_json_delta goes to text
// blocks as text and is dropped for tool-call blocks.
func TestR55StreamInputJSONDeltaRoutingAfterJSONResponseTool(t *testing.T) {
	sse := r55MsgStart +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"j\",\"name\":\"json\",\"input\":{}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"t\",\"name\":\"f\",\"input\":{}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"a\\\":1}\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{}\"}}\n\n"
	var texts []string
	for _, c := range r55AnthropicChunks(t, sse, true) {
		if c.Type == provider.ChunkTypeToolInputDelta {
			t.Fatalf("unexpected tool-input-delta %q", c.Text)
		}
		if c.Type == provider.ChunkTypeText {
			texts = append(texts, c.Text)
		}
	}
	if len(texts) != 1 || texts[0] != "{}" {
		t.Fatalf("text deltas = %q, want [\"{}\"]", texts)
	}
}
