package cohere

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func drainCohere(t *testing.T, body string) []*provider.StreamChunk {
	t.Helper()
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(body)))
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			return out
		}
		out = append(out, c)
	}
}

// TS emits text-start/text-end (id String(index)), empty text-deltas, and
// response-metadata for message-start even when the id is "".
func TestStreamTextBlocksAndEmptyDeltaR33(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"message-start","id":""}`,
		`data: {"type":"content-start","index":2,"delta":{"message":{"content":{"type":"text","text":""}}}}`,
		`data: {"type":"content-delta","index":2,"delta":{"message":{"content":{"text":""}}}}`,
		`data: {"type":"content-end","index":2}`,
	}, "\n\n") + "\n\n"
	got := drainCohere(t, body)
	if len(got) != 5 {
		t.Fatalf("got %d chunks: %+v", len(got), got)
	}
	if got[0].Type != provider.ChunkTypeResponseMetadata || got[0].ResponseMetadata == nil {
		t.Fatalf("chunk0 = %+v", got[0])
	}
	want := []struct {
		t  provider.ChunkType
		id string
	}{{provider.ChunkTypeTextStart, "2"}, {provider.ChunkTypeText, "2"}, {provider.ChunkTypeTextEnd, "2"}}
	for i, w := range want {
		if got[i+1].Type != w.t || got[i+1].ID != w.id {
			t.Fatalf("chunk%d = %+v want %v %s", i+1, got[i+1], w.t, w.id)
		}
	}
}

// TS keeps one pendingToolCall: the latest start wins for deltas/end.
func TestStreamSinglePendingToolCallR33(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"tool-call-start","index":0,"delta":{"message":{"tool_calls":{"id":"a","type":"function","function":{"name":"fa","arguments":""}}}}}`,
		`data: {"type":"tool-call-start","index":1,"delta":{"message":{"tool_calls":{"id":"b","type":"function","function":{"name":"fb","arguments":""}}}}}`,
		`data: {"type":"tool-call-delta","index":1,"delta":{"message":{"tool_calls":{"function":{"arguments":"{\"x\":1}"}}}}}`,
		`data: {"type":"tool-call-end","index":1}`,
	}, "\n\n") + "\n\n"
	for i := 0; i < 20; i++ {
		var call *provider.StreamChunk
		for _, c := range drainCohere(t, body) {
			if c.Type == provider.ChunkTypeToolCall {
				call = c
			}
		}
		if call == nil || call.ToolCall.ID != "b" {
			t.Fatalf("iteration %d: tool call = %+v", i, call)
		}
	}
}
