package anthropic

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func drainAnthropicChunks(t *testing.T, sse string) ([]*provider.StreamChunk, error) {
	t.Helper()
	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), false)
	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			return chunks, nil
		}
		if err != nil {
			return chunks, err
		}
		chunks = append(chunks, chunk)
	}
}

func finishChunks(chunks []*provider.StreamChunk) []*provider.StreamChunk {
	var out []*provider.StreamChunk
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			out = append(out, c)
		}
	}
	return out
}

const anthropicMsgStart = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\n"
const anthropicTextStart = "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"
const anthropicTextDelta = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n"
const anthropicTextStop = "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
const anthropicMsgDelta = "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n"
const anthropicMsgStop = "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

// TS #23814 / #23816: partial output without message_stop ends with an
// interrupted finish.
func TestStream_InterruptedAfterPartialOutput(t *testing.T) {
	chunks, err := drainAnthropicChunks(t, anthropicMsgStart+anthropicTextStart+anthropicTextDelta)
	if err != nil {
		t.Fatal(err)
	}
	finishes := finishChunks(chunks)
	if len(finishes) != 1 || finishes[0].FinishReason != types.FinishReasonInterrupted {
		t.Fatalf("finishes = %+v", finishes)
	}
	if finishes[0].Usage == nil {
		t.Fatal("interrupted finish must carry usage")
	}
}

// TS #23816: an empty stream keeps its empty-stream path (no finish chunk).
func TestStream_NoOutputKeepsEmptyStreamPath(t *testing.T) {
	for name, sse := range map[string]string{
		"nothing":       "",
		"message start": anthropicMsgStart,
		"empty block":   anthropicMsgStart + anthropicTextStart,
	} {
		chunks, err := drainAnthropicChunks(t, sse)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := finishChunks(chunks); len(got) != 0 {
			t.Fatalf("%s: synthesized finish %+v", name, got)
		}
	}
}

// TS #23816: a completed message keeps its finish reason; a body that ends
// after message_delta but before message_stop has no output since the last
// finish and so no synthesized finish.
func TestStream_CompletedMessageNotInterrupted(t *testing.T) {
	chunks, err := drainAnthropicChunks(t, anthropicMsgStart+anthropicTextStart+anthropicTextDelta+anthropicTextStop+anthropicMsgDelta+anthropicMsgStop)
	if err != nil {
		t.Fatal(err)
	}
	finishes := finishChunks(chunks)
	if len(finishes) != 1 || finishes[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("finishes = %+v", finishes)
	}
}

// TS #23814: a cutoff in a later message of a multi-message stream reports
// interrupted for that message while the first keeps its own finish.
func TestStream_InterruptedInLaterMessage(t *testing.T) {
	second := strings.Replace(anthropicMsgStart, "msg_1", "msg_2", 1)
	chunks, err := drainAnthropicChunks(t, anthropicMsgStart+anthropicTextStart+anthropicTextDelta+anthropicTextStop+anthropicMsgDelta+anthropicMsgStop+second+anthropicTextStart+anthropicTextDelta)
	if err != nil {
		t.Fatal(err)
	}
	finishes := finishChunks(chunks)
	if len(finishes) != 2 || finishes[0].FinishReason != types.FinishReasonStop || finishes[1].FinishReason != types.FinishReasonInterrupted {
		t.Fatalf("finishes = %+v", finishes)
	}
	if finishes[1].Usage == nil || len(finishes[1].ProviderMetadata) == 0 {
		t.Fatalf("interrupted finish should reuse the last usage and metadata: %+v", finishes[1])
	}
}

// TS #23814: a stream that reported an error keeps its error path.
func TestStream_ErrorSuppressesInterruptedFinish(t *testing.T) {
	errEvent := "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"
	chunks, err := drainAnthropicChunks(t, anthropicMsgStart+anthropicTextStart+anthropicTextDelta+errEvent)
	if err != nil {
		t.Fatal(err)
	}
	if got := finishChunks(chunks); len(got) != 0 {
		t.Fatalf("synthesized finish after error: %+v", got)
	}
}
