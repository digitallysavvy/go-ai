package streaming

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// SSEEvent represents a single Server-Sent Event
type SSEEvent struct {
	// Event type (e.g., "message", "error", "done")
	Event string

	// Event data
	Data string

	// Event ID (optional)
	ID string

	// Retry time in milliseconds (optional)
	Retry int
}

// sseScannerInitialBufferSize and sseScannerMaxLineSize bound the
// per-line buffer bufio.Scanner uses while splitting an SSE stream.
// bufio.Scanner otherwise caps every token (here, one SSE line) at
// bufio.MaxScanTokenSize (64 KiB) with no way to opt out, which is far
// smaller than realistic single-line SSE payloads: a large base64
// image/audio chunk, a long tool-call-arguments or reasoning delta, or any
// other sizeable JSON payload a provider emits unchunked on one `data: `
// line. TS's SSE parser (eventsource-parser, vendored under
// ai/packages/provider-utils/node_modules/eventsource-parser) has no
// line-length limit at all, so without this a stream that TS handles fine
// aborts in Go with "token too long" (bufio.ErrTooLong). We can't go
// unbounded in Go the way a buffered string accumulator can, so this
// mirrors the same 64 KiB start / 32 MiB ceiling already used for batch
// result NDJSON scanning elsewhere in the codebase (see e.g.
// pkg/providers/openai/batch.go, pkg/providers/anthropic/batch.go,
// pkg/providers/google/batch.go, pkg/providers/gateway/batch.go) so memory
// use stays bounded instead of growing per line without limit.
const (
	sseScannerInitialBufferSize = 64 * 1024
	sseScannerMaxLineSize       = 32 * 1024 * 1024
)

// SSEParser parses Server-Sent Events from a stream
type SSEParser struct {
	scanner *bufio.Scanner
	err     error
}

// NewSSEParser creates a new SSE parser for the given reader
func NewSSEParser(r io.Reader) *SSEParser {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, sseScannerInitialBufferSize), sseScannerMaxLineSize)
	return &SSEParser{
		scanner: scanner,
	}
}

// Next returns the next SSE event from the stream
// Returns io.EOF when the stream is complete
func (p *SSEParser) Next() (*SSEEvent, error) {
	if p.err != nil {
		return nil, p.err
	}

	event := &SSEEvent{}
	var dataLines []string

	for p.scanner.Scan() {
		line := p.scanner.Text()

		// Empty line signals end of event
		if line == "" {
			if len(dataLines) > 0 || event.Event != "" {
				// Combine data lines
				event.Data = strings.Join(dataLines, "\n")
				return event, nil
			}
			continue
		}

		// Parse the line
		if strings.HasPrefix(line, ":") {
			// Comment line, ignore
			continue
		}

		// Split by first colon
		colonIdx := strings.Index(line, ":")
		if colonIdx == -1 {
			// Treat as field with empty value
			continue
		}

		field := line[:colonIdx]
		value := line[colonIdx+1:]

		// Remove leading space from value
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}

		// Process field
		switch field {
		case "event":
			event.Event = value
		case "data":
			dataLines = append(dataLines, value)
		case "id":
			event.ID = value
		case "retry":
			// Parse retry as integer (milliseconds)
			var retry int
			_, _ = fmt.Sscanf(value, "%d", &retry)
			event.Retry = retry
		}
	}

	// Check for scanner error
	if err := p.scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			err = fmt.Errorf("sse: event line exceeds the %d byte maximum (%w)", sseScannerMaxLineSize, err)
		}
		p.err = err
		return nil, err
	}

	// Check if we have any data left
	if len(dataLines) > 0 || event.Event != "" {
		event.Data = strings.Join(dataLines, "\n")
		return event, nil
	}

	// End of stream
	p.err = io.EOF
	return nil, io.EOF
}

// Err returns any error that occurred during parsing
func (p *SSEParser) Err() error {
	if p.err == io.EOF {
		return nil
	}
	return p.err
}

// ParseSSEStream parses an entire SSE stream and returns all events
func ParseSSEStream(r io.Reader) ([]*SSEEvent, error) {
	parser := NewSSEParser(r)
	var events []*SSEEvent

	for {
		event, err := parser.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, nil
}

// SSEWriter writes Server-Sent Events to a writer
type SSEWriter struct {
	writer io.Writer
}

// NewSSEWriter creates a new SSE writer
func NewSSEWriter(w io.Writer) *SSEWriter {
	return &SSEWriter{writer: w}
}

// WriteEvent writes an SSE event to the stream
func (w *SSEWriter) WriteEvent(event SSEEvent) error {
	var buf bytes.Buffer

	// Write event type if present
	if event.Event != "" {
		_, _ = fmt.Fprintf(&buf, "event: %s\n", event.Event)
	}

	// Write ID if present
	if event.ID != "" {
		_, _ = fmt.Fprintf(&buf, "id: %s\n", event.ID)
	}

	// Write retry if present
	if event.Retry > 0 {
		_, _ = fmt.Fprintf(&buf, "retry: %d\n", event.Retry)
	}

	// Write data (can be multiple lines)
	if event.Data != "" {
		lines := strings.Split(event.Data, "\n")
		for _, line := range lines {
			_, _ = fmt.Fprintf(&buf, "data: %s\n", line)
		}
	}

	// Write empty line to signal end of event
	buf.WriteString("\n")

	// Write to underlying writer
	_, err := w.writer.Write(buf.Bytes())
	return err
}

// WriteData is a convenience method to write a data-only event
func (w *SSEWriter) WriteData(data string) error {
	return w.WriteEvent(SSEEvent{Data: data})
}

// WriteNamedEvent is a convenience method to write an event with a type and data
func (w *SSEWriter) WriteNamedEvent(eventType, data string) error {
	return w.WriteEvent(SSEEvent{
		Event: eventType,
		Data:  data,
	})
}

// WriteDone writes a "done" event to signal stream completion
func (w *SSEWriter) WriteDone() error {
	return w.WriteEvent(SSEEvent{
		Event: "done",
		Data:  "[DONE]",
	})
}

// IsStreamDone checks if an SSE event signals the stream is done
func IsStreamDone(event *SSEEvent) bool {
	return event.Data == "[DONE]" || event.Event == "done"
}
