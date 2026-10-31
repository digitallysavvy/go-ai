package anthropic

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"io"
	"net/http"
	"strings"
	"testing"
)

// encodeEventStreamMessage builds a raw AWS event-stream binary frame with
// the given string headers and payload, mirroring @smithy/eventstream-codec's
// EventStreamCodec.encode used by amazon-bedrock-anthropic-fetch.test.ts.
func encodeEventStreamMessage(t *testing.T, headers map[string]string, payload []byte) []byte {
	t.Helper()
	var headerBuf bytes.Buffer
	for name, value := range headers {
		headerBuf.WriteByte(byte(len(name)))
		headerBuf.WriteString(name)
		headerBuf.WriteByte(7) // string type
		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[:], uint16(len(value)))
		headerBuf.Write(lenBuf[:])
		headerBuf.WriteString(value)
	}
	headerBytes := headerBuf.Bytes()

	totalLength := uint32(12 + len(headerBytes) + len(payload) + 4)
	headersLength := uint32(len(headerBytes))

	prelude := make([]byte, 12)
	binary.BigEndian.PutUint32(prelude[0:4], totalLength)
	binary.BigEndian.PutUint32(prelude[4:8], headersLength)
	preludeCRC := crc32.ChecksumIEEE(prelude[0:8])
	binary.BigEndian.PutUint32(prelude[8:12], preludeCRC)

	var msg bytes.Buffer
	msg.Write(prelude)
	msg.Write(headerBytes)
	msg.Write(payload)

	messageCRC := crc32.ChecksumIEEE(msg.Bytes())
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], messageCRC)
	msg.Write(crcBuf[:])

	return msg.Bytes()
}

func TestEventStreamDecoder_ChunkEvent(t *testing.T) {
	anthropicEvent := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`
	chunkPayload := `{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(anthropicEvent)) + `"}`

	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, []byte(chunkPayload))

	decoder := NewEventStreamDecoder(bytes.NewReader(frame))
	event, err := decoder.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent: %v", err)
	}
	if event.MessageType != "event" || event.EventType != "chunk" {
		t.Fatalf("event = %#v", event)
	}
	if event.Data != chunkPayload {
		t.Fatalf("event.Data = %q, want %q", event.Data, chunkPayload)
	}
}

func TestEventStreamDecoder_ExceptionTypeCaptured(t *testing.T) {
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type":   "exception",
		":exception-type": "ThrottlingException",
	}, []byte(`{"message":"Rate limit exceeded"}`))

	decoder := NewEventStreamDecoder(bytes.NewReader(frame))
	event, err := decoder.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent: %v", err)
	}
	if event.MessageType != "exception" {
		t.Fatalf("MessageType = %q, want exception", event.MessageType)
	}
	if event.ExceptionType != "ThrottlingException" {
		t.Fatalf("ExceptionType = %q, want ThrottlingException", event.ExceptionType)
	}
}

// TestTransformEventStreamToSSE_ChunkEvent ports "should transform Bedrock
// event stream to SSE format".
// TestEventStreamDecoder_TruncatedFrameErrors verifies that switching to the
// shared pkg/providers/bedrock/eventstream decoder (WG-B1) gives
// Bedrock-Anthropic the "N buffered bytes remain" truncated-frame error for
// free (TS row 1ac9af8), rather than a generic io.ErrUnexpectedEOF.
func TestEventStreamDecoder_TruncatedFrameErrors(t *testing.T) {
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, []byte(`{"bytes":"aGVsbG8="}`))
	truncated := frame[:len(frame)-3]

	decoder := NewEventStreamDecoder(bytes.NewReader(truncated))
	_, err := decoder.ReadEvent()
	if err == nil {
		t.Fatal("expected an error for a truncated frame")
	}
	if !strings.Contains(err.Error(), "Incomplete Amazon Bedrock event-stream frame") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestEventStreamDecoder_CorruptFrameErrors verifies CRC mismatches are
// surfaced as decode errors rather than silently producing garbage output
// (TS row e0776b9).
func TestEventStreamDecoder_CorruptFrameErrors(t *testing.T) {
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, []byte(`{"bytes":"aGVsbG8="}`))
	corrupted := append([]byte{}, frame...)
	corrupted[len(corrupted)-5] ^= 0xFF

	decoder := NewEventStreamDecoder(bytes.NewReader(corrupted))
	if _, err := decoder.ReadEvent(); err == nil {
		t.Fatal("expected a CRC mismatch error for a corrupted frame")
	}
}

func TestTransformEventStreamToSSE_ChunkEvent(t *testing.T) {
	anthropicEvent := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`
	chunkPayload := `{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(anthropicEvent)) + `"}`
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, []byte(chunkPayload))

	out := transformEventStreamToSSE(io.NopCloser(bytes.NewReader(frame)), http.Header{})
	data, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	want := "data: " + anthropicEvent + "\n\n"
	if string(data) != want {
		t.Fatalf("got %q, want %q", string(data), want)
	}
}

// TestTransformEventStreamToSSE_MessageStop ports "should handle messageStop
// event and emit [DONE]".
func TestTransformEventStreamToSSE_MessageStop(t *testing.T) {
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "messageStop",
	}, []byte(`{}`))

	out := transformEventStreamToSSE(io.NopCloser(bytes.NewReader(frame)), http.Header{})
	data, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "data: [DONE]\n\n" {
		t.Fatalf("got %q, want %q", string(data), "data: [DONE]\n\n")
	}
}

// TestTransformEventStreamToSSE_Exception ports "should handle exception
// messages".
func TestTransformEventStreamToSSE_Exception(t *testing.T) {
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type":   "exception",
		":exception-type": "ThrottlingException",
	}, []byte(`{"message":"Rate limit exceeded"}`))

	out := transformEventStreamToSSE(io.NopCloser(bytes.NewReader(frame)), http.Header{})
	data, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`"type":"error"`,
		`"type":"ThrottlingException"`,
		`"message":"Rate limit exceeded"`,
	} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("output %q missing %q", got, want)
		}
	}
}

// TestTransformEventStreamToSSE_MultipleEvents ports "should handle multiple
// events in sequence".
func TestTransformEventStreamToSSE_MultipleEvents(t *testing.T) {
	event1 := `{"type":"message_start","message":{"id":"msg_123"}}`
	event2 := `{"type":"content_block_delta","delta":{"text":"Hi"}}`

	chunk1 := encodeEventStreamMessage(t, map[string]string{":message-type": "event", ":event-type": "chunk"},
		[]byte(`{"bytes":"`+base64.StdEncoding.EncodeToString([]byte(event1))+`"}`))
	chunk2 := encodeEventStreamMessage(t, map[string]string{":message-type": "event", ":event-type": "chunk"},
		[]byte(`{"bytes":"`+base64.StdEncoding.EncodeToString([]byte(event2))+`"}`))
	stop := encodeEventStreamMessage(t, map[string]string{":message-type": "event", ":event-type": "messageStop"}, []byte(`{}`))

	var combined bytes.Buffer
	combined.Write(chunk1)
	combined.Write(chunk2)
	combined.Write(stop)

	out := transformEventStreamToSSE(io.NopCloser(&combined), http.Header{})
	data, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	full := string(data)
	for _, want := range []string{"data: " + event1 + "\n\n", "data: " + event2 + "\n\n", "data: [DONE]\n\n"} {
		if !bytes.Contains([]byte(full), []byte(want)) {
			t.Errorf("output missing %q; got %q", want, full)
		}
	}
}

// TestTransformEventStreamToSSE_ChunkedNetworkRead ports "should handle
// chunked event data spanning multiple network chunks" by wrapping the frame
// in a reader that only returns a few bytes per Read call.
func TestTransformEventStreamToSSE_ChunkedNetworkRead(t *testing.T) {
	anthropicEvent := `{"type":"content_block_delta","delta":{"text":"Hello World"}}`
	chunkPayload := `{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(anthropicEvent)) + `"}`
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, []byte(chunkPayload))

	out := transformEventStreamToSSE(io.NopCloser(&slowReader{data: frame, chunkSize: 3}), http.Header{})
	data, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	want := "data: " + anthropicEvent + "\n\n"
	if string(data) != want {
		t.Fatalf("got %q, want %q", string(data), want)
	}
}

// TestTransformEventStreamToSSE_MissingBytesFallback ports "should handle
// chunk events with missing bytes field".
func TestTransformEventStreamToSSE_MissingBytesFallback(t *testing.T) {
	chunkPayload := `{"someOtherField":"value"}`
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, []byte(chunkPayload))

	out := transformEventStreamToSSE(io.NopCloser(bytes.NewReader(frame)), http.Header{})
	data, err := io.ReadAll(out)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	want := "data: " + chunkPayload + "\n\n"
	if string(data) != want {
		t.Fatalf("got %q, want %q", string(data), want)
	}
}

// slowReader returns at most chunkSize bytes per Read call, simulating
// network fragmentation of a single event-stream frame.
type slowReader struct {
	data      []byte
	chunkSize int
	pos       int
}

func (r *slowReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := r.chunkSize
	if n > len(p) {
		n = len(p)
	}
	if r.pos+n > len(r.data) {
		n = len(r.data) - r.pos
	}
	copy(p, r.data[r.pos:r.pos+n])
	r.pos += n
	return n, nil
}

// TS convertBase64ToUint8Array (atob) throws on invalid base64, which errors
// the stream; the raw payload is not forwarded.
func TestTransformEventStreamToSSE_InvalidBase64ErrorsStream(t *testing.T) {
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, []byte(`{"bytes":"!!!not-base64!!!"}`))
	out := transformEventStreamToSSE(io.NopCloser(bytes.NewReader(frame)), http.Header{})
	data, err := io.ReadAll(out)
	if err == nil {
		t.Fatalf("expected stream error, got data %q", data)
	}
	if bytes.Contains(data, []byte("not-base64")) {
		t.Fatalf("raw payload leaked: %q", data)
	}
}

// atob accepts unpadded and base64url input.
func TestTransformEventStreamToSSE_ForgivingBase64(t *testing.T) {
	// "hello?>" base64url unpadded = aGVsbG8_Pg
	frame := encodeEventStreamMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, []byte(`{"bytes":"aGVsbG8_Pg"}`))
	out := transformEventStreamToSSE(io.NopCloser(bytes.NewReader(frame)), http.Header{})
	data, err := io.ReadAll(out)
	if err != nil || string(data) != "data: hello?>\n\n" {
		t.Fatalf("got %q, %v", data, err)
	}
}

// TS reads `parsed.value.bytes` on whatever safeParseJSON returns: null throws,
// a truthy non-string bytes throws in convertBase64ToUint8Array, and every
// other value (number, string, array, falsy bytes) falls back to the payload.
func TestR55TransformEventStreamToSSE_NonObjectChunkPayloads(t *testing.T) {
	for _, tc := range []struct {
		payload string
		wantErr string
	}{
		{`null`, "Cannot read properties of null (reading 'bytes')"},
		{`{"bytes":5}`, "base64String.replace is not a function"},
		{`{"bytes":true}`, "base64String.replace is not a function"},
		{`{"bytes":{}}`, "base64String.replace is not a function"},
		{`5`, ""},
		{`"x"`, ""},
		{`[]`, ""},
		{`{"bytes":0}`, ""},
		{`{"bytes":null}`, ""},
	} {
		t.Run(tc.payload, func(t *testing.T) {
			frame := encodeEventStreamMessage(t, map[string]string{
				":message-type": "event",
				":event-type":   "chunk",
			}, []byte(tc.payload))
			out := transformEventStreamToSSE(io.NopCloser(bytes.NewReader(frame)), http.Header{})
			data, err := io.ReadAll(out)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if len(data) != 0 {
					t.Fatalf("data = %q, want none", data)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if want := "data: " + tc.payload + "\n\n"; string(data) != want {
				t.Fatalf("got %q, want %q", data, want)
			}
		})
	}
}
