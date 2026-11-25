package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS enqueues a finish part on every message_stop, with the stream-wide
// finish reason and usage, even when no message_start or message_delta came
// between two message_stop events.
func TestR61EveryMessageStopFinishes(t *testing.T) {
	model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w,
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"c\",\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n"+
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n"+
				"data: {\"type\":\"message_stop\"}\n\n"+
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"},\"usage\":{\"output_tokens\":7}}\n\n"+
				"data: {\"type\":\"message_stop\"}\n\n"+
				"data: {\"type\":\"message_stop\"}\n\n")
	})
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var finishes []*provider.StreamChunk
	for _, c := range drain(t, s) {
		if c.Type == provider.ChunkTypeFinish {
			finishes = append(finishes, c)
		}
	}
	if len(finishes) != 3 {
		t.Fatalf("finishes = %d, want 3", len(finishes))
	}
	want := []struct {
		reason types.FinishReason
		raw    string
		out    int64
	}{{types.FinishReasonStop, "end_turn", 5}, {types.FinishReasonLength, "max_tokens", 7}, {types.FinishReasonLength, "max_tokens", 7}}
	for i, f := range finishes {
		if f.FinishReason != want[i].reason || f.RawFinishReason != want[i].raw || f.Usage == nil || f.Usage.OutputTokens == nil || *f.Usage.OutputTokens != want[i].out {
			t.Fatalf("finish %d = %+v", i, f)
		}
	}
}
