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
)

// EventStreamDecoder decodes AWS EventStream binary format
type EventStreamDecoder struct {
	reader *bufio.Reader
}

// NewEventStreamDecoder creates a new event stream decoder
func NewEventStreamDecoder(r io.Reader) *EventStreamDecoder {
	return &EventStreamDecoder{
		reader: bufio.NewReader(r),
	}
}

// EventStreamEvent represents a decoded event from the stream
type EventStreamEvent struct {
	MessageType string // "event" or "exception"
	EventType   string // "chunk", "messageStop", etc.
	Data        string // Event payload as JSON string
}

// ReadEvent reads and decodes a single event from the stream
// Returns io.EOF when stream is complete
func (d *EventStreamDecoder) ReadEvent() (*EventStreamEvent, error) {
	// AWS EventStream format:
	// [total_length:4][headers_length:4][prelude_crc:4][headers][payload][message_crc:4]
	//
	// All integers are big-endian

	// Read prelude (12 bytes)
	prelude := make([]byte, 12)
	if _, err := io.ReadFull(d.reader, prelude); err != nil {
		return nil, err
	}

	totalLength := binary.BigEndian.Uint32(prelude[0:4])
	headersLength := binary.BigEndian.Uint32(prelude[4:8])
	preludeCRC := binary.BigEndian.Uint32(prelude[8:12])

	// Verify prelude CRC
	calculatedPreludeCRC := crc32.ChecksumIEEE(prelude[0:8])
	if calculatedPreludeCRC != preludeCRC {
		return nil, fmt.Errorf("prelude CRC mismatch: expected %d, got %d", preludeCRC, calculatedPreludeCRC)
	}

	// Calculate payload length
	// Total = prelude(12) + headers + payload + messageCRC(4)
	payloadLength := totalLength - 12 - headersLength - 4

	// Read headers
	headersBytes := make([]byte, headersLength)
	if _, err := io.ReadFull(d.reader, headersBytes); err != nil {
		return nil, fmt.Errorf("failed to read headers: %w", err)
	}

	// Parse headers
	headers, err := d.parseHeaders(headersBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse headers: %w", err)
	}

	// Read payload
	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(d.reader, payload); err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}

	// Read and verify message CRC
	messageCRCBytes := make([]byte, 4)
	if _, err := io.ReadFull(d.reader, messageCRCBytes); err != nil {
		return nil, fmt.Errorf("failed to read message CRC: %w", err)
	}
	messageCRC := binary.BigEndian.Uint32(messageCRCBytes)

	// Verify message CRC (covers everything except the CRC itself)
	messageData := make([]byte, 0, totalLength-4)
	messageData = append(messageData, prelude...)
	messageData = append(messageData, headersBytes...)
	messageData = append(messageData, payload...)
	calculatedMessageCRC := crc32.ChecksumIEEE(messageData)
	if calculatedMessageCRC != messageCRC {
		return nil, fmt.Errorf("message CRC mismatch: expected %d, got %d", messageCRC, calculatedMessageCRC)
	}

	// Build event
	event := &EventStreamEvent{
		MessageType: headers[":message-type"],
		EventType:   headers[":event-type"],
		Data:        string(payload),
	}

	return event, nil
}

// parseHeaders parses the headers from the event stream message
// Headers format: [header_name_length:1][header_name][header_value_type:1][header_value]
func (d *EventStreamDecoder) parseHeaders(data []byte) (map[string]string, error) {
	headers := make(map[string]string)
	reader := bytes.NewReader(data)

	for reader.Len() > 0 {
		// Read header name length (1 byte)
		var nameLength uint8
		if err := binary.Read(reader, binary.BigEndian, &nameLength); err != nil {
			return nil, fmt.Errorf("failed to read header name length: %w", err)
		}

		// Read header name
		name := make([]byte, nameLength)
		if _, err := io.ReadFull(reader, name); err != nil {
			return nil, fmt.Errorf("failed to read header name: %w", err)
		}

		// Read header value type (1 byte)
		// 7 = string
		var valueType uint8
		if err := binary.Read(reader, binary.BigEndian, &valueType); err != nil {
			return nil, fmt.Errorf("failed to read header value type: %w", err)
		}

		// Read header value based on type
		var value string
		switch valueType {
		case 7: // String
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

// TransformToSSE transforms Bedrock event stream to SSE format
func (d *EventStreamDecoder) TransformToSSE(w io.Writer) error {
	for {
		event, err := d.ReadEvent()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		// Handle different event types
		switch event.MessageType {
		case "event":
			switch event.EventType {
			case "chunk":
				// Parse chunk to extract base64-encoded bytes
				var chunkData struct {
					Bytes string `json:"bytes"`
				}
				if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
					return fmt.Errorf("failed to parse chunk data: %w", err)
				}

				// Decode base64 to get actual Anthropic event
				anthropicEvent, err := base64.StdEncoding.DecodeString(chunkData.Bytes)
				if err != nil {
					return fmt.Errorf("failed to decode base64 bytes: %w", err)
				}

				// Write as SSE event
				if _, err := fmt.Fprintf(w, "data: %s\n\n", string(anthropicEvent)); err != nil {
					return err
				}

			case "messageStop":
				// End of stream
				if _, err := fmt.Fprintf(w, "data: [DONE]\n\n"); err != nil {
					return err
				}
				return nil

			default:
				// Unknown event type, skip
			}

		case "exception":
			// Error event
			errorData := map[string]interface{}{
				"type":  "error",
				"error": event.Data,
			}
			errorJSON, _ := json.Marshal(errorData)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", string(errorJSON)); err != nil {
				return err
			}
			return fmt.Errorf("stream exception: %s", event.Data)

		default:
			// Unknown message type, skip
		}
	}
}

// SSEStreamReader wraps the event stream decoder to provide an io.Reader interface
type SSEStreamReader struct {
	decoder *EventStreamDecoder
	buffer  *bytes.Buffer
}

// NewSSEStreamReader creates a new SSE stream reader
func NewSSEStreamReader(r io.Reader) *SSEStreamReader {
	return &SSEStreamReader{
		decoder: NewEventStreamDecoder(r),
		buffer:  &bytes.Buffer{},
	}
}

// Read implements io.Reader interface
func (s *SSEStreamReader) Read(p []byte) (n int, err error) {
	// If buffer has data, read from it first
	if s.buffer.Len() > 0 {
		return s.buffer.Read(p)
	}

	// Read next event and transform to SSE
	event, err := s.decoder.ReadEvent()
	if err != nil {
		return 0, err
	}

	// Transform event to SSE format and write to buffer
	switch event.MessageType {
	case "event":
		switch event.EventType {
		case "chunk":
			var chunkData struct {
				Bytes string `json:"bytes"`
			}
			if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
				return 0, fmt.Errorf("failed to parse chunk data: %w", err)
			}

			anthropicEvent, err := base64.StdEncoding.DecodeString(chunkData.Bytes)
			if err != nil {
				return 0, fmt.Errorf("failed to decode base64 bytes: %w", err)
			}

			fmt.Fprintf(s.buffer, "data: %s\n\n", string(anthropicEvent))

		case "messageStop":
			fmt.Fprintf(s.buffer, "data: [DONE]\n\n")
			// After messageStop, return EOF on next read
			defer func() {
				if s.buffer.Len() == 0 {
					err = io.EOF
				}
			}()

		default:
			// Skip unknown event types
			return s.Read(p)
		}

	case "exception":
		errorData := map[string]interface{}{
			"type":  "error",
			"error": event.Data,
		}
		errorJSON, _ := json.Marshal(errorData)
		fmt.Fprintf(s.buffer, "data: %s\n\n", string(errorJSON))

	default:
		// Skip unknown message types
		return s.Read(p)
	}

	// Read from buffer
	return s.buffer.Read(p)
}

// Close implements io.Closer interface
func (s *SSEStreamReader) Close() error {
	return nil
}
