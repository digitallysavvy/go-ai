package anthropic

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// skipMessageStartChunk consumes the anthropic.message_start custom chunk
// that every stream now begins with (TS #21979).
func skipMessageStartChunk(t *testing.T, stream *anthropicStream) {
	t.Helper()
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() message_start chunk error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeCustom || chunk.CustomContent == nil || chunk.CustomContent.Kind != "anthropic.message_start" {
		t.Fatalf("expected anthropic.message_start custom chunk, got %#v", chunk)
	}
}

func TestStreamEmitsMessageStartCustomChunk(t *testing.T) {
	sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-haiku-4-5-20251001\",\"content\":[],\"usage\":{\"input_tokens\":849,\"cache_creation_input_tokens\":0,\"cache_read_input_tokens\":0,\"cache_creation\":{\"ephemeral_5m_input_tokens\":0,\"ephemeral_1h_input_tokens\":0},\"output_tokens\":10,\"service_tier\":\"standard\"}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), false)

	chunk, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if chunk.Type != provider.ChunkTypeCustom || chunk.CustomContent == nil || chunk.CustomContent.Kind != "anthropic.message_start" {
		t.Fatalf("first chunk = %#v", chunk)
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(chunk.CustomContent.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	a := meta["anthropic"]
	if a["id"] != "msg_01" || a["model"] != "claude-haiku-4-5-20251001" {
		t.Fatalf("metadata = %v", a)
	}
	usage, _ := a["usage"].(map[string]interface{})
	if usage["input_tokens"] != float64(849) || usage["output_tokens"] != float64(10) || usage["service_tier"] != "standard" {
		t.Fatalf("usage = %v", usage)
	}
	if _, ok := usage["cache_creation"].(map[string]interface{}); !ok {
		t.Fatalf("usage should pass through nested cache_creation: %v", usage)
	}
}
