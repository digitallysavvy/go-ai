package streaming

import (
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// An OnExtraDelta hook that handles an event but has nothing to emit must not
// make Next return a nil chunk with a nil error.
func TestOpenAICompatStream_ExtraDeltaHandledWithoutChunkSkipsEvent(t *testing.T) {
	s := newTestStream("data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n")
	calls := 0
	s.OnExtraDelta = func([]byte) (*provider.StreamChunk, bool) {
		calls++
		return nil, calls == 1
	}
	var types []provider.ChunkType
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			t.Fatal("Next returned a nil chunk with a nil error")
		}
		types = append(types, c.Type)
	}
	if len(types) == 0 || types[len(types)-1] != provider.ChunkTypeFinish {
		t.Fatalf("chunk types = %v", types)
	}
}
