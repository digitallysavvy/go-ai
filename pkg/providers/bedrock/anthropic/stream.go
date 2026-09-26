package anthropic

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
)

// EventStreamDecoder decodes the AWS event-stream binary format used by
// Bedrock's invoke-with-response-stream action.
//
// NOTE: This is a self-contained copy for the Bedrock-Anthropic package. The
// PRD's WG-B1 work group (Bedrock Converse rewrite, out of this change's
// scope) is expected to move a hardened version of this decoder to a shared
// pkg/providers/bedrock/eventstream package — see HANDOFF.md rows 1ac9af8
// (truncated-frame error) and 8ca1352 (exception-type propagation richness).
// This copy captures :exception-type (needed for a useful error message
// here) but does not yet implement the "N buffered bytes remain" truncation
// error or the modeled-exception-to-stream-error metadata mapping
// (getAmazonBedrockStreamErrorMetadata); both are deferred to WG-B1.
type EventStreamDecoder struct {
	reader *bufio.Reader
}

// NewEventStreamDecoder creates a new event stream decoder.
func NewEventStreamDecoder(r io.Reader) *EventStreamDecoder {
	return &EventStreamDecoder{reader: bufio.NewReader(r)}
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
	// AWS EventStream format:
	// [total_length:4][headers_length:4][prelude_crc:4][headers][payload][message_crc:4]
	// All integers are big-endian.
	prelude := make([]byte, 12)
	if _, err := io.ReadFull(d.reader, prelude); err != nil {
		return nil, err
	}

	totalLength := binary.BigEndian.Uint32(prelude[0:4])
	headersLength := binary.BigEndian.Uint32(prelude[4:8])
	preludeCRC := binary.BigEndian.Uint32(prelude[8:12])

	calculatedPreludeCRC := crc32.ChecksumIEEE(prelude[0:8])
	if calculatedPreludeCRC != preludeCRC {
		return nil, fmt.Errorf("prelude CRC mismatch: expected %d, got %d", preludeCRC, calculatedPreludeCRC)
	}

	payloadLength := totalLength - 12 - headersLength - 4

	headersBytes := make([]byte, headersLength)
	if _, err := io.ReadFull(d.reader, headersBytes); err != nil {
		return nil, fmt.Errorf("failed to read headers: %w", err)
	}

	headers, err := d.parseHeaders(headersBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse headers: %w", err)
	}

	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(d.reader, payload); err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}

	messageCRCBytes := make([]byte, 4)
	if _, err := io.ReadFull(d.reader, messageCRCBytes); err != nil {
		return nil, fmt.Errorf("failed to read message CRC: %w", err)
	}
	messageCRC := binary.BigEndian.Uint32(messageCRCBytes)

	messageData := make([]byte, 0, totalLength-4)
	messageData = append(messageData, prelude...)
	messageData = append(messageData, headersBytes...)
	messageData = append(messageData, payload...)
	calculatedMessageCRC := crc32.ChecksumIEEE(messageData)
	if calculatedMessageCRC != messageCRC {
		return nil, fmt.Errorf("message CRC mismatch: expected %d, got %d", messageCRC, calculatedMessageCRC)
	}

	return &EventStreamEvent{
		MessageType:   headers[":message-type"],
		EventType:     headers[":event-type"],
		ExceptionType: headers[":exception-type"],
		Data:          string(payload),
	}, nil
}

// parseHeaders parses the headers section of an event stream message.
// Format: [header_name_length:1][header_name][header_value_type:1][header_value]
func (d *EventStreamDecoder) parseHeaders(data []byte) (map[string]string, error) {
	headers := make(map[string]string)
	reader := bytes.NewReader(data)

	for reader.Len() > 0 {
		var nameLength uint8
		if err := binary.Read(reader, binary.BigEndian, &nameLength); err != nil {
			return nil, fmt.Errorf("failed to read header name length: %w", err)
		}
		name := make([]byte, nameLength)
		if _, err := io.ReadFull(reader, name); err != nil {
			return nil, fmt.Errorf("failed to read header name: %w", err)
		}

		var valueType uint8
		if err := binary.Read(reader, binary.BigEndian, &valueType); err != nil {
			return nil, fmt.Errorf("failed to read header value type: %w", err)
		}

		var value string
		switch valueType {
		case 7: // string
			var valueLength uint16
			if err := binary.Read(reader, binary.BigEndian, &valueLength); err != nil {
				return nil, fmt.Errorf("failed to read header value length: %w", err)
			}
			valueBytes := make([]byte, valueLength)
			if _, err := io.ReadFull(reader, valueBytes); err != nil {
				return nil, fmt.Errorf("failed to read header value: %w", err)
			}
			value = string(valueBytes)
		default:
			return nil, fmt.Errorf("unsupported header value type: %d", valueType)
		}

		headers[string(name)] = value
	}

	return headers, nil
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
