package ai

import (
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// The first chunk is emitted without delay (TS initialDelayInMs defaults to
// 0); only chunks after the first wait chunkDelayInMs.
func TestSimulateReadableStreamNoDelayBeforeFirstChunk(t *testing.T) {
	stream := SimulateReadableStream([]provider.StreamChunk{{Text: "a"}, {Text: "b"}}, 300*time.Millisecond)
	start := time.Now()
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("first chunk took %v, want no delay", elapsed)
	}
	if _, err := stream.Next(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("second chunk after %v, want the chunk delay", elapsed)
	}
}
