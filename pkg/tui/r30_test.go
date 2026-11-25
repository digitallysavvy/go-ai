package tui

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// A transport failure is sent on errs before both channels are closed; the
// stream must report it every time, not only when the select happens to pick
// errs over the closed chunk channel.
func TestR30TransportStreamReportsErrorSentBeforeClose(t *testing.T) {
	boom := errors.New("Failed to fetch chat: 500")
	for i := 0; i < 200; i++ {
		chunks := make(chan ai.UIMessageChunk)
		errs := make(chan error, 1)
		errs <- boom
		close(errs)
		close(chunks)
		stream := newTransportTextStream(chunks, errs)
		if _, err := stream.Next(); !errors.Is(err, boom) {
			t.Fatalf("iteration %d: Next err = %v, want %v", i, err, boom)
		}
	}
}
