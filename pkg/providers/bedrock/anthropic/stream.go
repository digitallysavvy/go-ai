package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock/eventstream"
)

// EventStreamDecoder decodes the AWS event-stream binary format used by
// Bedrock's invoke-with-response-stream action. It is a thin adapter over the
// shared pkg/providers/bedrock/eventstream decoder (built for WG-B1's
// Converse rewrite), so Bedrock-Anthropic and the Converse client share a
// single, hardened implementation: prelude/message CRC verification, a
// "N buffered bytes remain" truncated-frame error, and decode errors are
// always surfaced rather than silently dropped (TS rows 1ac9af8 / e0776b9).
type EventStreamDecoder struct {
	decoder *eventstream.Decoder
}

// NewEventStreamDecoder creates a new event stream decoder.
func NewEventStreamDecoder(r io.Reader) *EventStreamDecoder {
	return &EventStreamDecoder{decoder: eventstream.NewDecoder(r)}
}

// EventStreamEvent represents a decoded event from the stream.
type EventStreamEvent struct {
	MessageType   string // "event" or "exception"
	EventType     string // ":event-type" header, e.g. "chunk", "messageStop"
	ExceptionType string // ":exception-type" header, e.g. "throttlingException"
	Data          string // Event payload as a JSON string
}

// ReadEvent reads and decodes a single event from the stream.
// Returns io.EOF when the stream is complete.
func (d *EventStreamDecoder) ReadEvent() (*EventStreamEvent, error) {
	event, err := d.decoder.Next()
	if err != nil {
		return nil, err
	}
	return &EventStreamEvent{
		MessageType:   event.MessageType,
		EventType:     event.EventType,
		ExceptionType: event.ExceptionType,
		Data:          string(event.Data),
	}, nil
}

// transformEventStreamToSSE wraps a Bedrock event-stream response body,
// producing a text/event-stream-formatted reader for the shared Anthropic SSE
// parser. Mirrors amazon-bedrock-anthropic-fetch.ts
// transformAmazonBedrockEventStreamToSSE (ai@7.0.113). Registered as
// anthropic.Config.TransformStreamBody.
func transformEventStreamToSSE(body io.ReadCloser, _ http.Header) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer body.Close() //nolint:errcheck
		decoder := NewEventStreamDecoder(body)
		var err error
		for {
			var event *EventStreamEvent
			event, err = decoder.ReadEvent()
			if err != nil {
				break
			}
			if writeErr := writeSSEEvent(pw, event); writeErr != nil {
				err = writeErr
				break
			}
		}
		if err == io.EOF {
			err = nil
		}
		_ = pw.CloseWithError(err)
	}()
	return pr
}

// writeSSEEvent writes one decoded Bedrock event-stream event as an SSE
// "data: ...\n\n" line.
func writeSSEEvent(w io.Writer, event *EventStreamEvent) error {
	switch event.MessageType {
	case "event":
		switch event.EventType {
		case "chunk":
			var chunkData struct {
				Bytes string `json:"bytes"`
			}
			if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil || chunkData.Bytes == "" {
				// Malformed or bytes-less chunk: fall back to the raw payload,
				// matching TS's fallback behavior.
				_, err := fmt.Fprintf(w, "data: %s\n\n", event.Data)
				return err
			}
			decoded, err := base64.StdEncoding.DecodeString(chunkData.Bytes)
			if err != nil {
				_, ferr := fmt.Fprintf(w, "data: %s\n\n", event.Data)
				return ferr
			}
			_, err = fmt.Fprintf(w, "data: %s\n\n", string(decoded))
			return err
		case "messageStop":
			_, err := fmt.Fprint(w, "data: [DONE]\n\n")
			return err
		}
		return nil

	case "exception":
		var data interface{}
		message := event.Data
		if err := json.Unmarshal([]byte(event.Data), &data); err == nil {
			if m, ok := data.(map[string]interface{}); ok {
				if msg, ok := m["message"].(string); ok {
					message = msg
				}
			}
		} else {
			data = event.Data
		}
		errType := event.EventType
		if errType == "" {
			errType = event.ExceptionType
		}
		if errType == "" {
			errType = "error"
		}
		payload := map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    errType,
				"message": message,
				"data":    data,
			},
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", string(encoded))
		return err
	}
	return nil
}
