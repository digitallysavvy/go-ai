package openai

import (
	"io"
	"strings"
	"testing"
	"time"

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

// TestR77ProvbCreatedAtZeroIsEpoch: TS sets the response timestamp to
// new Date(created_at * 1000), so created_at 0 is the epoch; Go treated 0 as
// missing. An absent created_at has no timestamp.
func TestR77ProvbCreatedAtZeroIsEpoch(t *testing.T) {
	for _, tc := range []struct {
		data  string
		event bool
		ok    bool
	}{
		{`{"id":"r","created_at":0}`, false, true},
		{`{"type":"response.created","response":{"id":"r","created_at":0}}`, true, true},
		{`{"type":"response.created","response":{"id":"r","created_at":1.5}}`, true, true},
		{`{"id":"r"}`, false, false},
		{`{"type":"response.created","response":{"id":"r","created_at":null}}`, true, false},
	} {
		ts, ok := responsesCreatedAt([]byte(tc.data), tc.event)
		if ok != tc.ok {
			t.Errorf("%s: ok = %v", tc.data, ok)
			continue
		}
		if ok && strings.Contains(tc.data, `":0}`) && !ts.Equal(time.UnixMilli(0)) {
			t.Errorf("%s: timestamp = %v, want the epoch", tc.data, ts)
		}
		if ok && strings.Contains(tc.data, "1.5") && ts.UnixMilli() != 1500 {
			t.Errorf("%s: timestamp = %v, want 1.5 s", tc.data, ts)
		}
	}
	ev := func(s string) string { return "data: " + s + "\n\n" }
	s := newResponsesStream(io.NopCloser(strings.NewReader(ev(`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","created_at":0,"model":"m"}}`))), false)
	c, err := s.Next()
	for err == nil && c.Type != provider.ChunkTypeResponseMetadata {
		c, err = s.Next()
	}
	if err != nil || c.ResponseMetadata == nil || !c.ResponseMetadata.Timestamp.Equal(time.UnixMilli(0)) || c.ResponseMetadata.Timestamp.IsZero() {
		t.Fatalf("response metadata = %+v, %v; want the epoch timestamp", c, err)
	}
}
