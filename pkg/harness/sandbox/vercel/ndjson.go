package vercel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
)

// connectionError is @vercel/sandbox 3.9.0 `ConnectionError`
// (api-client.js): the response stream was interrupted, or ended inside a
// record, so the command may still be running and its output can be read
// again. Callers recover from it (getLogsStream retries, runCommand falls
// back to the logs and the command endpoints); any other stream error is
// final.
type connectionError struct {
	message string
	cause   error
}

func (e *connectionError) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}

func (e *connectionError) Unwrap() error { return e.cause }

// errStopJSONLines ends readJSONLines early without an error.
var errStopJSONLines = errors.New("stop")

// readJSONLines reads a JSON Lines body the way 3.9.0 `readJsonLines` does
// with the `jsonlines` 0.1.1 parser (`emitInvalidLines: true`): the text is
// split on "\n" only, an exactly empty line is skipped, and every other line
// must be one JSON value (JSON whitespace such as a trailing "\r" around it
// is allowed; a line of only whitespace is not). An invalid line is a final
// syntax error, except an unterminated last line, which means the stream
// ended inside a record (a connectionError). A read failure is a
// connectionError too. Lines are bounded by vercelNDJSONMaxLine (a Go
// limit; see wrapScanError). yield gets each line's bytes; errStopJSONLines
// stops reading.
func readJSONLines(body io.Reader, sessionID string, yield func(line []byte) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), vercelNDJSONMaxLine)
	unterminated := false
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			return i + 1, data[:i], nil
		}
		if atEOF && len(data) > 0 {
			unterminated = true
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if !json.Valid(line) {
			if unterminated {
				return &connectionError{message: "Stream ended with an incomplete JSON record"}
			}
			return fmt.Errorf("vercel sandbox session %q: invalid JSON line in the ndjson stream: %q", sessionID, truncateForError(line))
		}
		if err := yield(line); err != nil {
			if errors.Is(err, errStopJSONLines) {
				return nil
			}
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return wrapScanError(err, sessionID)
		}
		return &connectionError{message: "Stream connection was interrupted", cause: err}
	}
	return nil
}

func truncateForError(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}

// logLine is one 3.9.0 `LogLine`: {stream: "stdout" | "stderr", data:
// string} or {stream: "error", data: {code, message}}.
type logLine struct {
	Stream string
	Data   string
	Err    *StreamError
}

// parseLogLine is `LogLine.parse` (a discriminated union on stream).
func parseLogLine(raw []byte, sessionID string) (logLine, error) {
	var line struct {
		Stream *string          `json:"stream"`
		Data   *json.RawMessage `json:"data"`
	}
	// jsonlines: plain JSON.parse of each line, then LogLine.parse.
	if err := jsonexact.UnmarshalPlain(raw, &line); err != nil || line.Stream == nil || line.Data == nil {
		return logLine{}, invalidLogLine(raw)
	}
	switch *line.Stream {
	case "stdout", "stderr":
		var data string
		if err := json.Unmarshal(*line.Data, &data); err != nil {
			return logLine{}, invalidLogLine(raw)
		}
		return logLine{Stream: *line.Stream, Data: data}, nil
	case "error":
		var data struct {
			Code    *string `json:"code"`
			Message *string `json:"message"`
		}
		if err := jsonexact.UnmarshalPlain(*line.Data, &data); err != nil || data.Code == nil || data.Message == nil {
			return logLine{}, invalidLogLine(raw)
		}
		return logLine{Stream: "error", Err: &StreamError{Code: *data.Code, Message: *data.Message, SessionID: sessionID}}, nil
	}
	return logLine{}, invalidLogLine(raw)
}

func invalidLogLine(raw []byte) error {
	return fmt.Errorf("vercel sandbox: invalid log line: %s", truncateForError(raw))
}

// jsLength is JS `string.length` (UTF-16 code units).
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// jsSliceFrom is JS `string.slice(start)` in UTF-16 code units.
func jsSliceFrom(s string, start int) string {
	if start <= 0 {
		return s
	}
	units := utf16.Encode([]rune(s))
	if start >= len(units) {
		return ""
	}
	return string(utf16.Decode(units[start:]))
}

// logDelivered counts the UTF-16 code units of each stream already handed
// to the caller (3.9.0 `delivered`), so a reconnected log stream skips them.
type logDelivered map[string]int

// sleepCtx waits d or until ctx is done.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// logsReconnectDelay is the 3.9.0 getLogsStream retry delay, 200 ms * 2^attempt.
var logsReconnectDelay = func(attempt int) time.Duration {
	return time.Duration(200<<attempt) * time.Millisecond
}
