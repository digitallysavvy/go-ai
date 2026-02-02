package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"strings"
	"testing"
)

// buildEventStreamMessage builds a test AWS EventStream message
func buildEventStreamMessage(messageType, eventType, payload string) []byte {
	// Build headers
	headers := buildHeaders(map[string]string{
		":message-type": messageType,
		":event-type":   eventType,
	})

	// Calculate lengths
	headersLength := uint32(len(headers))
	payloadBytes := []byte(payload)
	payloadLength := uint32(len(payloadBytes))
	totalLength := 12 + headersLength + payloadLength + 4 // prelude + headers + payload + crc

	// Build prelude
	prelude := make([]byte, 12)
	binary.BigEndian.PutUint32(prelude[0:4], totalLength)
	binary.BigEndian.PutUint32(prelude[4:8], headersLength)
	preludeCRC := crc32.ChecksumIEEE(prelude[0:8])
	binary.BigEndian.PutUint32(prelude[8:12], preludeCRC)

	// Build message
	message := make([]byte, 0, totalLength)
	message = append(message, prelude...)
	message = append(message, headers...)
	message = append(message, payloadBytes...)

	// Calculate and append message CRC
	messageCRC := crc32.ChecksumIEEE(message)
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, messageCRC)
	message = append(message, crcBytes...)

	return message
}

// buildHeaders builds the headers section
func buildHeaders(headers map[string]string) []byte {
	var buf bytes.Buffer

	for name, value := range headers {
		// Header name length (1 byte)
		buf.WriteByte(byte(len(name)))
		// Header name
		buf.WriteString(name)
		// Header value type (7 = string)
		buf.WriteByte(7)
		// Header value length (2 bytes, big-endian)
		valueLengthBytes := make([]byte, 2)
		binary.BigEndian.PutUint16(valueLengthBytes, uint16(len(value)))
		buf.Write(valueLengthBytes)
		// Header value
		buf.WriteString(value)
	}

	return buf.Bytes()
}

func TestEventStreamDecoder_ReadEvent(t *testing.T) {
	// Create a test chunk event
	anthropicEvent := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`
	chunkPayload := map[string]string{
		"bytes": base64.StdEncoding.EncodeToString([]byte(anthropicEvent)),
	}
	chunkPayloadJSON, _ := json.Marshal(chunkPayload)

	message := buildEventStreamMessage("event", "chunk", string(chunkPayloadJSON))

	// Create decoder
	decoder := NewEventStreamDecoder(bytes.NewReader(message))

	// Read event
	event, err := decoder.ReadEvent()
	if err != nil {
		t.Fatalf("failed to read event: %v", err)
	}

	// Verify event
	if event.MessageType != "event" {
		t.Errorf("expected message type 'event', got '%s'", event.MessageType)
	}
	if event.EventType != "chunk" {
		t.Errorf("expected event type 'chunk', got '%s'", event.EventType)
	}

	// Parse chunk data
	var chunkData struct {
		Bytes string `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
		t.Fatalf("failed to parse chunk data: %v", err)
	}

	// Decode base64
	decodedBytes, err := base64.StdEncoding.DecodeString(chunkData.Bytes)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}

	if string(decodedBytes) != anthropicEvent {
		t.Errorf("expected decoded data '%s', got '%s'", anthropicEvent, string(decodedBytes))
	}
}

func TestEventStreamDecoder_MessageStop(t *testing.T) {
	message := buildEventStreamMessage("event", "messageStop", "{}")

	decoder := NewEventStreamDecoder(bytes.NewReader(message))

	event, err := decoder.ReadEvent()
	if err != nil {
		t.Fatalf("failed to read event: %v", err)
	}

	if event.MessageType != "event" {
		t.Errorf("expected message type 'event', got '%s'", event.MessageType)
	}
	if event.EventType != "messageStop" {
		t.Errorf("expected event type 'messageStop', got '%s'", event.EventType)
	}
}

func TestEventStreamDecoder_Exception(t *testing.T) {
	message := buildEventStreamMessage("exception", "error", `{"message":"test error"}`)

	decoder := NewEventStreamDecoder(bytes.NewReader(message))

	event, err := decoder.ReadEvent()
	if err != nil {
		t.Fatalf("failed to read event: %v", err)
	}

	if event.MessageType != "exception" {
		t.Errorf("expected message type 'exception', got '%s'", event.MessageType)
	}
}

func TestEventStreamDecoder_EOF(t *testing.T) {
	decoder := NewEventStreamDecoder(bytes.NewReader([]byte{}))

	_, err := decoder.ReadEvent()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestSSEStreamReader(t *testing.T) {
	// Create test events
	anthropicEvent1 := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`
	chunkPayload1 := map[string]string{
		"bytes": base64.StdEncoding.EncodeToString([]byte(anthropicEvent1)),
	}
	chunkPayloadJSON1, _ := json.Marshal(chunkPayload1)
	message1 := buildEventStreamMessage("event", "chunk", string(chunkPayloadJSON1))

	anthropicEvent2 := `{"type":"content_block_delta","delta":{"type":"text_delta","text":" World"}}`
	chunkPayload2 := map[string]string{
		"bytes": base64.StdEncoding.EncodeToString([]byte(anthropicEvent2)),
	}
	chunkPayloadJSON2, _ := json.Marshal(chunkPayload2)
	message2 := buildEventStreamMessage("event", "chunk", string(chunkPayloadJSON2))

	messageStop := buildEventStreamMessage("event", "messageStop", "{}")

	// Combine messages
	var buf bytes.Buffer
	buf.Write(message1)
	buf.Write(message2)
	buf.Write(messageStop)

	// Create SSE stream reader
	reader := NewSSEStreamReader(&buf)

	// Read all data
	data, err := io.ReadAll(reader)
	if err != nil && err != io.EOF {
		t.Fatalf("failed to read stream: %v", err)
	}

	// Verify output
	output := string(data)
	if !strings.Contains(output, "data: "+anthropicEvent1) {
		t.Errorf("expected output to contain first event")
	}
	if !strings.Contains(output, "data: "+anthropicEvent2) {
		t.Errorf("expected output to contain second event")
	}
	if !strings.Contains(output, "data: [DONE]") {
		t.Errorf("expected output to contain [DONE]")
	}
}

func TestTransformToSSE(t *testing.T) {
	// Create test chunk event
	anthropicEvent := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"Test"}}`
	chunkPayload := map[string]string{
		"bytes": base64.StdEncoding.EncodeToString([]byte(anthropicEvent)),
	}
	chunkPayloadJSON, _ := json.Marshal(chunkPayload)
	message := buildEventStreamMessage("event", "chunk", string(chunkPayloadJSON))

	messageStop := buildEventStreamMessage("event", "messageStop", "{}")

	// Combine messages
	var input bytes.Buffer
	input.Write(message)
	input.Write(messageStop)

	// Create decoder
	decoder := NewEventStreamDecoder(&input)

	// Transform to SSE
	var output bytes.Buffer
	err := decoder.TransformToSSE(&output)
	if err != nil {
		t.Fatalf("failed to transform to SSE: %v", err)
	}

	// Verify output
	outputStr := output.String()
	if !strings.Contains(outputStr, "data: "+anthropicEvent) {
		t.Errorf("expected output to contain event data")
	}
	if !strings.Contains(outputStr, "data: [DONE]") {
		t.Errorf("expected output to contain [DONE]")
	}
}
