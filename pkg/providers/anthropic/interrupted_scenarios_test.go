package anthropic

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23816 'only emits an interrupted finish after output': every scenario of
// the TS table, at the stream level.
func TestStream_InterruptedOnlyAfterOutput(t *testing.T) {
	msgStart := func(id, content string) string {
		return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"" + id + "\",\"model\":\"claude\",\"content\":" + content + ",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n"
	}
	textDelta := func(s string) string {
		return "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + s + "\"}}\n\n"
	}
	thinkingStart := "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n"
	thinkingDelta := func(s string) string {
		return "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"" + s + "\"}}\n\n"
	}
	toolStart := "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool-1\",\"name\":\"test-tool\",\"input\":{}}}\n\n"
	toolDelta := func(s string) string {
		return "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"" + s + "\"}}\n\n"
	}
	prevMessage := msgStart("msg-previous", "[]") + anthropicTextStart + textDelta("Previous answer") + anthropicTextStop + anthropicMsgDelta + anthropicMsgStop

	cases := []struct {
		name string
		sse  string
		want []types.FinishReason
	}{
		{"empty", "", nil},
		{"metadata-only", msgStart("m", "[]"), nil},
		{"empty-text", msgStart("m", "[]") + anthropicTextStart + textDelta(""), nil},
		{"empty-reasoning", msgStart("m", "[]") + thinkingStart + thinkingDelta(""), nil},
		{"empty-tool-input", msgStart("m", "[]") + toolStart + toolDelta(""), nil},
		{"later-message", prevMessage + msgStart("msg-current", "[]"), []types.FinishReason{types.FinishReasonStop}},
		{"partial-text", msgStart("m", "[]") + anthropicTextStart + textDelta("Partial"), []types.FinishReason{types.FinishReasonInterrupted}},
		{"partial-reasoning", msgStart("m", "[]") + thinkingStart + thinkingDelta("Thinking"), []types.FinishReason{types.FinishReasonInterrupted}},
		{"partial-tool-input", msgStart("m", "[]") + toolStart + toolDelta("{"), []types.FinishReason{types.FinishReasonInterrupted}},
		{"tool-call", msgStart("m", `[{"type":"tool_use","id":"tool-1","name":"test-tool","input":{}}]`), []types.FinishReason{types.FinishReasonInterrupted}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunks, err := drainAnthropicChunks(t, tc.sse)
			if err != nil {
				t.Fatal(err)
			}
			var got []types.FinishReason
			for _, f := range finishChunks(chunks) {
				got = append(got, f.FinishReason)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("finishes = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("finishes = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
