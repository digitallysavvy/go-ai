package ai

import (
	"context"
	"testing"
)

// TestCreateUIMessageStreamWithOptions_LateWriteAndMergeAreDropped checks
// that a writer kept past Execute's return can still call Write and Merge
// without panicking (send on a closed channel). Late chunks are dropped.
func TestCreateUIMessageStreamWithOptions_LateWriteAndMergeAreDropped(t *testing.T) {
	writerCh := make(chan UIMessageStreamWriter, 1)
	out, errCh := CreateUIMessageStreamWithOptions(context.Background(), UIMessageStreamOptions{
		Execute: func(w UIMessageStreamWriter) {
			w.Write(UIMessageChunk{"type": "start"})
			writerCh <- w
		},
	})
	for range out {
	}
	for range errCh {
	}

	w := <-writerCh
	w.Write(UIMessageChunk{"type": "text-start", "id": "late"})
	late := make(chan UIMessageChunk, 1)
	late <- UIMessageChunk{"type": "text-delta", "id": "late", "delta": "x"}
	close(late)
	w.Merge(late)
}
