package streaming

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	started bool
	// unterminated is set by the split function when the token it just
	// returned is the stream's final line with no line terminator after it.
	unterminated bool
}

// scanSSELines splits on "\n", "\r\n" and a bare "\r", the three line
// terminators of the SSE spec (bufio.ScanLines knows only the first two).
// The stream's final bytes with no terminator after them (including a lone
// trailing "\r", which eventsource-parser holds back as a possible CRLF half)
// come back as one last token with p.unterminated set.
func (p *SSEParser) scanSSELines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	i := bytes.IndexAny(data, "\r\n")
	if i < 0 {
		if atEOF {
			p.unterminated = true
			return len(data), data, nil
		}
		return 0, nil, nil
	}
	if data[i] == '\n' {
		return i + 1, data[:i], nil
	}
	// Bare CR or the CR of a CRLF pair: look at the next byte to tell them apart.
	if i+1 < len(data) {
		if data[i+1] == '\n' {
			return i + 2, data[:i], nil
		}
		return i + 1, data[:i], nil
	}
	if atEOF {
		p.unterminated = true
		return i + 1, data[:i], nil
	}
	return 0, nil, nil
}

// NewSSEParser creates a new SSE parser for the given reader
func NewSSEParser(r io.Reader) *SSEParser {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, sseScannerInitialBufferSize), sseScannerMaxLineSize)
	p := &SSEParser{scanner: scanner}
	scanner.Split(p.scanSSELines)
	return p
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
		if p.unterminated {
			// The final line has no terminator: eventsource-parser keeps it
			// as an incomplete line, and EventSourceParserStream (3.0.8, the
			// version TS provider-utils resolves) has no flush, so it is
			// never parsed.
			break
		}
		line := p.scanner.Text()
		if !p.started {
			// eventsource-parser strips a leading byte order mark.
			p.started = true
			line = strings.TrimPrefix(line, "\ufeff")
		}

		// Empty line signals end of event. Like eventsource-parser, an event
		// is dispatched only when it has a data field; an empty "data:" line
		// dispatches an event with empty data (the consumer's JSON parse then
		// fails, as in TS), while an event with no data field is dropped.
		if line == "" {
			if len(dataLines) > 0 {
				// Combine data lines
				event.Data = strings.Join(dataLines, "\n")
				return event, nil
			}
			// Nothing to dispatch: the event type, id and retry are reset.
			event = &SSEEvent{}
			continue
		}

		// Parse the line
		if strings.HasPrefix(line, ":") {
			// Comment line, ignore
			continue
		}

		// Split by first colon
		colonIdx := strings.Index(line, ":")
		field := line
		value := ""
		if colonIdx != -1 {
			// A line without a colon is a field with an empty value.
			field = line[:colonIdx]
			value = line[colonIdx+1:]
		}

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

	// End of stream. An event still pending (data lines with no closing
	// blank line) is dropped: eventsource-parser dispatches only on a blank
	// line, and EventSourceParserStream never flushes or resets the parser
	// when its input ends.
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

// RawChunkValue is the value a raw stream part carries for an SSE data
// payload (TS `rawValue: chunk.rawValue` from parseJsonEventStream): the
// parsed JSON, or nil when the payload is not valid JSON, as safeParseJSON
// returns `rawValue: undefined` for a JSONParseError. A payload that parses
// but fails the chunk schema keeps its parsed value.
func RawChunkValue(data string) interface{} {
	var raw interface{}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return nil
	}
	return raw
}

// IsStreamDone checks if an SSE event signals the stream is done
func IsStreamDone(event *SSEEvent) bool {
	return event.Data == "[DONE]" || event.Event == "done"
}
