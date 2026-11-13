package streaming

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS providers emit `{type:'raw', rawValue: chunk.rawValue}`, and
// safeParseJSON's rawValue is undefined when the payload is not JSON.
func TestRawChunkValueIsNilForUnparsablePayload(t *testing.T) {
	if got := RawChunkValue(`{"a":1}`); !reflect.DeepEqual(got, map[string]interface{}{"a": float64(1)}) {
		t.Fatalf("parsed = %#v", got)
	}
	for _, in := range []string{"not json", "", `{"a":`} {
		if got := RawChunkValue(in); got != nil {
			t.Fatalf("RawChunkValue(%q) = %#v, want nil", in, got)
		}
	}
}

func TestOpenAICompatStreamRawChunkForUnparsablePayloadIsNil(t *testing.T) {
	stream := newTestStream("data: not json\n\ndata: [DONE]\n\n")
	stream.IncludeRawChunks = true
	defer stream.Close() //nolint:errcheck

	chunks := collectStreamChunks(t, stream)
	if len(chunks) < 2 || chunks[0].Type != provider.ChunkTypeRaw || chunks[1].Type != provider.ChunkTypeError {
		var types []provider.ChunkType
		for _, c := range chunks {
			types = append(types, c.Type)
		}
		t.Fatalf("chunks = %v, want raw then error", types)
	}
	if chunks[0].Raw != nil {
		t.Fatalf("raw = %#v, want nil", chunks[0].Raw)
	}
}
