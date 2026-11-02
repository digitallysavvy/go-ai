package openai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR77ProvbStreamSkipsDoneMarker: TS parseJsonEventStream ignores a
// `data: [DONE]` frame and the finish part is enqueued when the stream ends
// (flush()), so events after the marker are still transformed. Go finished
// the stream at the marker and dropped them.
func TestR77ProvbStreamSkipsDoneMarker(t *testing.T) {
	ev := func(s string) string { return "data: " + s + "\n\n" }
	sse := ev(`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","created_at":1,"model":"m"}}`) +
		ev(`{"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"one","sequence_number":1}`) +
		ev(`[DONE]`) +
		ev(`{"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"two","sequence_number":2}`) +
		ev(`{"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`)
	s := newResponsesStream(io.NopCloser(strings.NewReader(sse)), false)
	var text string
	var finishes []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeFinish:
			finishes = append(finishes, c)
		}
	}
	if text != "onetwo" {
		t.Errorf("text = %q, want the delta after [DONE] too", text)
	}
	if len(finishes) != 1 || finishes[0].FinishReason != types.FinishReasonStop {
		t.Fatalf("finishes = %+v, want one stop finish at the end", finishes)
	}
	if u := finishes[0].Usage; u == nil || u.OutputTokens == nil || *u.OutputTokens != 2 {
		t.Errorf("usage = %+v, want response.completed's", u)
	}
}
