package streaming

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestSSEParserNextAndErr(t *testing.T) {
	input := strings.Join([]string{
		": comment ignored",
		"event: update",
		"id: evt-1",
		"retry: 1500",
		"data: hello",
		"data: world",
		"",
		"event: done",
		"data: [DONE]",
	}, "\n")

	p := NewSSEParser(strings.NewReader(input))

	e1, err := p.Next()
	if err != nil {
		t.Fatalf("first Next() error = %v", err)
	}
	if e1.Event != "update" || e1.ID != "evt-1" || e1.Retry != 1500 || e1.Data != "hello\nworld" {
		t.Fatalf("unexpected first event: %+v", e1)
	}

	e2, err := p.Next()
	if err != nil {
		t.Fatalf("second Next() error = %v", err)
	}
	if e2.Event != "done" || e2.Data != "[DONE]" {
		t.Fatalf("unexpected second event: %+v", e2)
	}

	if _, err := p.Next(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
	if p.Err() != nil {
		t.Fatalf("Err() after EOF should be nil, got %v", p.Err())
	}
}

func TestParseSSEStream(t *testing.T) {
	input := "data: a\n\ndata: b\n\n"
	events, err := ParseSSEStream(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseSSEStream() error = %v", err)
	}
	if len(events) != 2 || events[0].Data != "a" || events[1].Data != "b" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestSSEWriterAndHelpers(t *testing.T) {
	var buf bytes.Buffer
	w := NewSSEWriter(&buf)

	if err := w.WriteEvent(SSEEvent{
		Event: "update",
		ID:    "evt-1",
		Retry: 1000,
		Data:  "line1\nline2",
	}); err != nil {
		t.Fatalf("WriteEvent() error = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "event: update\n") ||
		!strings.Contains(out, "id: evt-1\n") ||
		!strings.Contains(out, "retry: 1000\n") ||
		!strings.Contains(out, "data: line1\n") ||
		!strings.Contains(out, "data: line2\n\n") {
		t.Fatalf("unexpected SSE output:\n%s", out)
	}

	buf.Reset()
	if err := w.WriteData("payload"); err != nil {
		t.Fatalf("WriteData() error = %v", err)
	}
	if !strings.Contains(buf.String(), "data: payload\n\n") {
		t.Fatalf("unexpected WriteData output: %q", buf.String())
	}

	buf.Reset()
	if err := w.WriteNamedEvent("message", "hello"); err != nil {
		t.Fatalf("WriteNamedEvent() error = %v", err)
	}
	if !strings.Contains(buf.String(), "event: message\n") || !strings.Contains(buf.String(), "data: hello\n\n") {
		t.Fatalf("unexpected WriteNamedEvent output: %q", buf.String())
	}

	buf.Reset()
	if err := w.WriteDone(); err != nil {
		t.Fatalf("WriteDone() error = %v", err)
	}
	if !strings.Contains(buf.String(), "event: done\n") || !strings.Contains(buf.String(), "data: [DONE]\n\n") {
		t.Fatalf("unexpected WriteDone output: %q", buf.String())
	}
}

func TestIsStreamDone(t *testing.T) {
	if !IsStreamDone(&SSEEvent{Data: "[DONE]"}) {
		t.Fatal("expected [DONE] to be recognized")
	}
	if !IsStreamDone(&SSEEvent{Event: "done"}) {
		t.Fatal("expected done event to be recognized")
	}
	if IsStreamDone(&SSEEvent{Event: "message", Data: "hello"}) {
		t.Fatal("did not expect regular event to be done")
	}
}
