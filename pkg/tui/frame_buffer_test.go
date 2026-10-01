package tui

import (
	"strings"
	"testing"
)

func TestTerminalFrameBufferFullRefreshAndDiff(t *testing.T) {
	out := &recordingWriter{}
	useSync := false
	buffer := NewTerminalFrameBuffer(out, TerminalFrameBufferOptions{UseSynchronizedUpdates: &useSync})

	buffer.Present("one\ntwo\nthree")
	if got := out.String(); got != "\x1b[H\x1b[2Jone\ntwo\nthree" {
		t.Fatalf("first frame = %q", got)
	}

	buffer.Present("one\nchanged\nthree")
	if got := out.Last(); got != "\x1b[2;1H\x1b[2Kchanged" {
		t.Fatalf("diff frame = %q", got)
	}

	buffer.Present("one\nchanged\nthree")
	if len(out.chunks) != 2 {
		t.Fatalf("unchanged frame wrote %d chunks, want 2", len(out.chunks))
	}

	buffer.Present("one")
	if got := out.Last(); got != "\x1b[2;1H\x1b[2K\x1b[3;1H\x1b[2K" {
		t.Fatalf("short frame clear = %q", got)
	}
}

func TestTerminalFrameBufferSynchronizedByDefault(t *testing.T) {
	out := &recordingWriter{}
	buffer := NewTerminalFrameBuffer(out)

	buffer.Present("frame")

	if got := out.String(); got != "\x1b[?2026h\x1b[H\x1b[2Jframe\x1b[?2026l" {
		t.Fatalf("frame = %q", got)
	}
}

type recordingWriter struct {
	chunks []string
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.chunks = append(w.chunks, string(p))
	return len(p), nil
}

func (w *recordingWriter) String() string {
	return strings.Join(w.chunks, "")
}

func (w *recordingWriter) Last() string {
	if len(w.chunks) == 0 {
		return ""
	}
	return w.chunks[len(w.chunks)-1]
}
