package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports packages/anthropic/src/map-anthropic-stop-reason.test.ts (ai@7.0.118)
// finish-reason table: pause_turn/end_turn/stop_sequence -> stop,
// refusal -> content-filter, max_tokens/model_context_window_exceeded ->
// length, tool_use -> tool-calls, compaction/unknown -> other.

func TestAnthropicStopReasonMapping_NonStreaming(t *testing.T) {
	cases := []struct {
		stopReason string
		want       types.FinishReason
	}{
		{"end_turn", types.FinishReasonStop},
		{"pause_turn", types.FinishReasonStop},
		{"stop_sequence", types.FinishReasonStop},
		{"refusal", types.FinishReasonContentFilter},
		{"max_tokens", types.FinishReasonLength},
		{"model_context_window_exceeded", types.FinishReasonLength},
		{"compaction", types.FinishReasonOther},
		{"some_unknown_reason", types.FinishReasonOther},
	}

	for _, tc := range cases {
		t.Run(tc.stopReason, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				body := map[string]interface{}{
					"id":            "msg_123",
					"type":          "message",
					"role":          "assistant",
					"content":       []map[string]interface{}{{"type": "text", "text": "hi"}},
					"model":         "claude-sonnet-4-5",
					"stop_reason":   tc.stopReason,
					"stop_sequence": nil,
					"usage":         map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()

			p := New(Config{APIKey: "test-key", BaseURL: server.URL})
			model, err := p.LanguageModel("claude-sonnet-4-5")
			if err != nil {
				t.Fatalf("LanguageModel() error: %v", err)
			}

			result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt:    types.Prompt{Text: "Test prompt"},
				MaxTokens: intPtr(100),
			})
			if err != nil {
				t.Fatalf("DoGenerate() error: %v", err)
			}
			if result.FinishReason != tc.want {
				t.Errorf("stop_reason=%q: FinishReason = %q, want %q", tc.stopReason, result.FinishReason, tc.want)
			}
		})
	}
}

func TestAnthropicStopReasonMapping_Streaming(t *testing.T) {
	cases := []struct {
		stopReason string
		want       types.FinishReason
	}{
		{"end_turn", types.FinishReasonStop},
		{"pause_turn", types.FinishReasonStop},
		{"stop_sequence", types.FinishReasonStop},
		{"refusal", types.FinishReasonContentFilter},
		{"max_tokens", types.FinishReasonLength},
		{"model_context_window_exceeded", types.FinishReasonLength},
		{"compaction", types.FinishReasonOther},
		{"some_unknown_reason", types.FinishReasonOther},
	}

	for _, tc := range cases {
		t.Run(tc.stopReason, func(t *testing.T) {
			body := sseBody([]sseEntry{
				{
					event: "message_start",
					data:  `{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
				},
				{
					event: "content_block_start",
					data:  `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				},
				{
					event: "content_block_delta",
					data:  `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
				},
				{
					event: "content_block_stop",
					data:  `{"type":"content_block_stop","index":0}`,
				},
				{
					event: "message_delta",
					data:  `{"type":"message_delta","delta":{"stop_reason":"` + tc.stopReason + `"},"usage":{"output_tokens":5}}`,
				},
				{
					event: "message_stop",
					data:  `{"type":"message_stop"}`,
				},
			})

			stream := newAnthropicStream(body, false)
			chunks := drainStream(t, stream)

			finishChunks := filterChunks(chunks, provider.ChunkTypeFinish)
			if len(finishChunks) != 1 {
				t.Fatalf("stop_reason=%q: expected exactly 1 finish chunk, got %d", tc.stopReason, len(finishChunks))
			}
			if got := finishChunks[0].FinishReason; got != tc.want {
				t.Errorf("stop_reason=%q: FinishReason = %q, want %q", tc.stopReason, got, tc.want)
			}
		})
	}
}
