package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS renderStream stops the session (leaves the alternate screen) when the
// stream was interrupted, even with continueSession set.
func TestInterruptedStreamStopsSessionEvenWhenContinuing(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	frame := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})
	input := newTestKeyInput()
	stream := newBlockingTextStream()
	source := NewStreamRenderSourceFromTextStream(stream, nil, types.StepResult{})
	renderer := NewTerminalRenderer(TerminalRendererOptions{Input: input, Output: out, FrameBuffer: frame, Columns: 48, Rows: 12})
	done := make(chan error, 1)
	go func() {
		_, err := renderer.RenderStream(context.Background(), AgentTUIStreamResult{Stream: source}, TerminalSessionOptions{Title: "T", ContinueSession: true, WaitForExit: false})
		done <- err
	}()
	select {
	case <-stream.started:
	case <-time.After(30 * time.Second):
		t.Fatal("stream did not start")
	}
	input.Push(TerminalKey{Type: TerminalKeyCtrlC})
	select {
	case err := <-done:
		if !errors.Is(err, errInterrupted) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("RenderStream did not interrupt")
	}
	if !strings.Contains(out.String(), "\x1b[?1049l") {
		t.Fatalf("alternate screen not left after interrupt: %q", out.String())
	}
}
