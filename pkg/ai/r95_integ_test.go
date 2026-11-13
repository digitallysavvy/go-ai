package ai

import "testing"

// A stopped SSE writer writes nothing more, even when a chunk (or a
// keep-alive tick) is ready at the same moment: TS cancel releases the reader
// and clears the keep-alive interval at once. Before the fix, select picked
// the ready chunk half the time, and TestR84streamKeepAliveManyIdleComments
// failed once under load with one keep-alive past the stop (2502 events;
// review round 95, the unidentified pkg/ai race failure).
func TestR95integSSEWriterStopWinsOverReadyChunk(t *testing.T) {
	for i := 0; i < 64; i++ {
		chunks := make(chan UIMessageChunk, 8)
		for j := 0; j < 8; j++ {
			chunks <- UIMessageChunk{"type": "text-delta", "id": "t", "delta": "x"}
		}
		stop := make(chan struct{})
		events := 0
		writeFailed, err := writeUISSEEvents(chunks, 0, false, stop, nil, func([]byte) error {
			events++
			if events == 1 {
				close(stop)
			}
			return nil
		})
		if writeFailed || err != nil {
			t.Fatalf("writeFailed = %v, err = %v", writeFailed, err)
		}
		if events != 1 {
			t.Fatalf("run %d: %d events written, want 1 (nothing after the stop)", i, events)
		}
	}
}
