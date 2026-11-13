package xai

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TestR77ProvbTSCasesUsage replays convert-xai-responses-usage.test.ts
// through convertXAIResponsesUsage (the TS wire usage decoded as the
// Responses stream and response bodies decode it).
func TestR77ProvbTSCasesUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		b, _ := json.Marshal(args[0])
		var u responses.ResponsesAPIUsage
		if err := json.Unmarshal(b, &u); err != nil {
			return nil, nil, err
		}
		return tsfixture.UsageToTS(convertXAIResponsesUsage(&u)), nil, nil
	}, nil)
}

// TestR77ProvbStreamSkipsDoneMarker: TS parseJsonEventStream ignores a
// `data: [DONE]` frame and flush() enqueues the finish part when the stream
// ends, so events after the marker are still transformed. Go finished the
// stream at the marker.
func TestR77ProvbStreamSkipsDoneMarker(t *testing.T) {
	ev := func(s string) string { return "data: " + s + "\n\n" }
	sse := ev(`{"type":"response.created","response":{"id":"resp_1","object":"response","output":[],"created_at":1,"model":"grok-4"}}`) +
		ev(`{"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"one"}`) +
		ev(`[DONE]`) +
		ev(`{"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"two"}`) +
		ev(`{"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","output":[],"created_at":1,"model":"grok-4","usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`)
	s := newXAIResponsesStream(io.NopCloser(strings.NewReader(sse)))
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
}

// TestR77ProvbCreatedAtZeroIsEpoch: TS getResponseMetadata passes created_at
// through createLanguageModelResponseMetadata, so 0 is the epoch (null or
// absent: no timestamp); Go treated 0 as missing.
func TestR77ProvbCreatedAtZeroIsEpoch(t *testing.T) {
	if ts, ok := xaiResponsesCreatedAt([]byte(`{"created_at":0}`), false); !ok || !ts.Equal(time.UnixMilli(0)) {
		t.Errorf("created_at 0 = %v, %v", ts, ok)
	}
	if _, ok := xaiResponsesCreatedAt([]byte(`{"response":{"created_at":null}}`), true); ok {
		t.Error("null created_at has a timestamp")
	}
	s := newXAIResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.created","response":{"id":"resp_1","object":"response","output":[],"created_at":0,"model":"grok-4"}}` + "\n\n")))
	c, err := s.Next()
	for err == nil && c.Type != provider.ChunkTypeResponseMetadata {
		c, err = s.Next()
	}
	if err != nil || c.ResponseMetadata == nil || c.ResponseMetadata.Timestamp.IsZero() || !c.ResponseMetadata.Timestamp.Equal(time.UnixMilli(0)) {
		t.Fatalf("response metadata = %+v, %v; want the epoch timestamp", c, err)
	}
}
