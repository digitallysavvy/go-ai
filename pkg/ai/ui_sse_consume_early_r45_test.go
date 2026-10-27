package ai

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// A ConsumeSSEStream callback that returns without reading everything must
// not stall the response: the remaining side-channel bytes are discarded.
func TestPipeUIMessageChunksConsumeSSEStreamReturningEarlyDoesNotBlock(t *testing.T) {
	chunks := make(chan UIMessageChunk, 3)
	chunks <- UIMessageChunk{"type": "start"}
	chunks <- UIMessageChunk{"type": "text-start", "id": "t"}
	chunks <- UIMessageChunk{"type": "finish"}
	close(chunks)
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- PipeUIMessageChunksToResponse(chunks, &out, &UIMessageStreamResponseInit{
			ConsumeSSEStream: func(io.Reader) error { return nil },
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the response never finished")
	}
	if !strings.HasSuffix(out.String(), "data: [DONE]\n\n") {
		t.Fatalf("body = %q", out.String())
	}
}
