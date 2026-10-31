package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r55HasSignatureDelta(chunks []*provider.StreamChunk) bool {
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeReasoning && c.ProviderMetadata != nil {
			return true
		}
	}
	return false
}

const (
	r55ThinkingStart = "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n"
	r55SignatureAt0  = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"s\"}}\n\n"
)

// TS clears blockType on every content_block_stop, so a signature delta after
// the thinking block stopped is dropped.
func TestR55StreamSignatureDeltaDroppedAfterBlockStop(t *testing.T) {
	sse := r55MsgStart + r55ThinkingStart +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" + r55SignatureAt0
	if r55HasSignatureDelta(r55AnthropicChunks(t, sse, false)) {
		t.Fatal("signature delta emitted after content_block_stop")
	}
	// Control: without the stop it is emitted.
	if !r55HasSignatureDelta(r55AnthropicChunks(t, r55MsgStart+r55ThinkingStart+r55SignatureAt0, false)) {
		t.Fatal("signature delta not emitted while the thinking block is open")
	}
}

// TS handles a fallback block before it sets blockType, so a fallback start
// leaves a thinking blockType in place.
func TestR55StreamFallbackStartKeepsThinkingBlockType(t *testing.T) {
	sse := r55MsgStart + r55ThinkingStart +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"fallback\",\"from\":{\"model\":\"a\"},\"to\":{\"model\":\"b\"}}}\n\n" + r55SignatureAt0
	if !r55HasSignatureDelta(r55AnthropicChunks(t, sse, false)) {
		t.Fatal("signature delta dropped after a fallback block start")
	}
}

// Without the JSON response tool, an input_json_delta only reaches tool-call
// blocks; on a text block it is dropped.
func TestR55StreamInputJSONDeltaOnTextBlockDropped(t *testing.T) {
	sse := r55MsgStart +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"a\\\":1}\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
	for _, c := range r55AnthropicChunks(t, sse, false) {
		if c.Type == provider.ChunkTypeToolInputDelta || c.Type == provider.ChunkTypeToolCall || (c.Type == provider.ChunkTypeText && c.Text != "") {
			t.Fatalf("input_json_delta on a text block produced %s %q", c.Type, c.Text)
		}
	}
}
