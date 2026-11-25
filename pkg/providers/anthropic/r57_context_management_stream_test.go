package anthropic

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// collectFinishChunks runs sse through an anthropicStream and returns every
// finish chunk.
func collectFinishChunks(t *testing.T, sse string) []*provider.StreamChunk {
	t.Helper()
	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), false)
	var finishes []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			return finishes
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finishes = append(finishes, chunk)
		}
	}
}

// finishMetaField returns providerMetadata.anthropic[key] of a finish chunk.
func finishMetaField(t *testing.T, chunk *provider.StreamChunk, key string) json.RawMessage {
	t.Helper()
	var meta map[string]map[string]json.RawMessage
	if err := json.Unmarshal(chunk.ProviderMetadata, &meta); err != nil {
		t.Fatalf("decode provider metadata: %v", err)
	}
	return meta["anthropic"][key]
}

// TestStreamContextManagementCarriesAcrossMessages: TS keeps contextManagement
// stream-wide (set by a message_delta with context_management, never reset),
// so a later message whose message_stop has no message_delta, or whose
// message_delta has no context_management, still reports the earlier value.
func TestStreamContextManagementCarriesAcrossMessages(t *testing.T) {
	start := func(id string) string {
		return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"" + id + "\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"c\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"
	}
	stop := "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	sse := start("m1") +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1},\"context_management\":{\"applied_edits\":[{\"type\":\"clear_thinking_20251015\",\"cleared_thinking_turns\":1,\"cleared_input_tokens\":4}]}}\n\n" +
		stop +
		start("m2") + stop +
		start("m3") +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
		stop

	finishes := collectFinishChunks(t, sse)
	if len(finishes) != 3 {
		t.Fatalf("finish chunks = %d, want 3", len(finishes))
	}
	want := `{"appliedEdits":[{"clearedInputTokens":4,"clearedThinkingTurns":1,"type":"clear_thinking_20251015"}]}`
	for i, f := range finishes {
		if got := string(finishMetaField(t, f, "contextManagement")); got != want {
			t.Errorf("finish %d contextManagement = %s, want %s", i, got, want)
		}
		if f.ContextManagement == nil {
			t.Errorf("finish %d ContextManagement = nil, want the first message's value", i)
		}
	}
}

// TestStreamMessageDeltaRawUsageStripsNestedKeys: TS's message_delta usage is
// a loose object, but its iterations items and output_tokens_details are plain
// z.object schemas, so unknown keys there are stripped from the raw usage
// (providerMetadata.anthropic.usage and usage.raw); top-level extras stay.
func TestStreamMessageDeltaRawUsageStripsNestedKeys(t *testing.T) {
	sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"c\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2,\"service_tier\":\"standard\",\"output_tokens_details\":{\"thinking_tokens\":1,\"extra\":1},\"iterations\":[{\"type\":\"message\",\"input_tokens\":1,\"output_tokens\":2,\"extra\":true}]}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	finishes := collectFinishChunks(t, sse)
	if len(finishes) != 1 {
		t.Fatalf("finish chunks = %d, want 1", len(finishes))
	}
	want := `{"input_tokens":1,"iterations":[{"input_tokens":1,"output_tokens":2,"type":"message"}],"output_tokens":2,"output_tokens_details":{"thinking_tokens":1},"service_tier":"standard"}`
	if got := string(finishMetaField(t, finishes[0], "usage")); got != want {
		t.Errorf("providerMetadata.anthropic.usage = %s, want %s", got, want)
	}
	raw, _ := json.Marshal(finishes[0].Usage.Raw)
	if string(raw) != want {
		t.Errorf("usage.raw = %s, want %s", raw, want)
	}
}
