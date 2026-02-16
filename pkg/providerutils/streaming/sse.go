package streaming

import (
	"bufio"
	"bytes"
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

// SSEParser parses Server-Sent Events from a stream
type SSEParser struct {
	scanner *bufio.Scanner
	err     error
}

// NewSSEParser creates a new SSE parser for the given reader
func NewSSEParser(r io.Reader) *SSEParser {
	return &SSEParser{
		scanner: bufio.NewScanner(r),
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
		buf.WriteString(fmt.Sprintf("event: %s\n", event.Event))
	}

	// Write ID if present
	if event.ID != "" {
		buf.WriteString(fmt.Sprintf("id: %s\n", event.ID))
	}

	// Write retry if present
	if event.Retry > 0 {
		buf.WriteString(fmt.Sprintf("retry: %d\n", event.Retry))
	}

	// Write data (can be multiple lines)
	if event.Data != "" {
		lines := strings.Split(event.Data, "\n")
		for _, line := range lines {
			buf.WriteString(fmt.Sprintf("data: %s\n", line))
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
