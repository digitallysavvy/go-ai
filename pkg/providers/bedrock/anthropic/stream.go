package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
			// TS convertBase64ToUint8Array (atob) throws on invalid base64,
			// which errors the stream; it does not fall back to the payload.
			decoded, err := convertBase64ToBytes(chunkData.Bytes)
			if err != nil {
				return fmt.Errorf("invalid base64 in bedrock chunk bytes: %w", err)
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
		errBody := map[string]interface{}{
			"type":    errType,
			"message": message,
			"data":    data,
		}
		// TS spreads getAmazonBedrockStreamErrorMetadata(type) into the error.
		if status, retryable, ok := bedrockStreamErrorMetadata(errType); ok {
			errBody["statusCode"] = status
			errBody["isRetryable"] = retryable
		}
		payload := map[string]interface{}{
			"type":  "error",
			"error": errBody,
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

// convertBase64ToBytes ports TS convertBase64ToUint8Array: base64url
// characters are mapped to the standard alphabet and the result is decoded
// with the WHATWG forgiving-base64 rules of atob (ASCII whitespace ignored,
// padding optional, length 1 mod 4 or any non-alphabet byte is an error).
func convertBase64ToBytes(s string) ([]byte, error) {
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\t', '\n', '\f', '\r', ' ':
			continue
		}
		b.WriteByte(s[i])
	}
	data := b.String()
	if len(data)%4 == 0 {
		data = strings.TrimSuffix(strings.TrimSuffix(data, "="), "=")
	}
	if len(data)%4 == 1 {
		return nil, base64.CorruptInputError(len(data))
	}
	return base64.RawStdEncoding.DecodeString(data)
}

// bedrockStreamErrorMetadata ports TS getAmazonBedrockStreamErrorMetadata.
func bedrockStreamErrorMetadata(errType string) (statusCode int, isRetryable bool, ok bool) {
	switch errType {
	case "internalServerException", "InternalServerException":
		return 500, true, true
	case "modelStreamErrorException", "ModelStreamErrorException":
		return 424, true, true
	case "serviceUnavailableException", "ServiceUnavailableException":
		return 503, true, true
	case "throttlingException", "ThrottlingException":
		return 429, true, true
	case "validationException", "ValidationException":
		return 400, false, true
	}
	return 0, false, false
}
