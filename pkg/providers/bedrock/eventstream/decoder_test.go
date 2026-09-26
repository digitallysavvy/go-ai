package eventstream

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"strings"
	"testing"
)

// buildFrame constructs a valid AWS event-stream binary frame with the given
// headers and payload, mirroring the format the real Bedrock Converse-stream
// endpoint emits.
func buildFrame(t *testing.T, headers map[string]string, payload []byte) []byte {
	t.Helper()

	var headerBytes bytes.Buffer
	for name, value := range headers {
		headerBytes.WriteByte(byte(len(name)))
		headerBytes.WriteString(name)
		headerBytes.WriteByte(7) // string type
		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[:], uint16(len(value)))
		headerBytes.Write(lenBuf[:])
		headerBytes.WriteString(value)
	}

	headersLen := uint32(headerBytes.Len())
	totalLength := 12 + headersLen + uint32(len(payload)) + 4

	var buf bytes.Buffer
	prelude := make([]byte, 8)
	binary.BigEndian.PutUint32(prelude[0:4], totalLength)
	binary.BigEndian.PutUint32(prelude[4:8], headersLen)
	buf.Write(prelude)
	preludeCRC := crc32.ChecksumIEEE(prelude)
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], preludeCRC)
	buf.Write(crcBuf[:])
	buf.Write(headerBytes.Bytes())
	buf.Write(payload)

	messageCRC := crc32.ChecksumIEEE(buf.Bytes())
	binary.BigEndian.PutUint32(crcBuf[:], messageCRC)
	buf.Write(crcBuf[:])

	if uint32(buf.Len()) != totalLength {
		t.Fatalf("test bug: built frame length %d != computed totalLength %d", buf.Len(), totalLength)
	}
	return buf.Bytes()
}

func TestDecoder_DecodesEventFrame(t *testing.T) {
	frame := buildFrame(t, map[string]string{
		":message-type": "event",
		":event-type":   "contentBlockDelta",
	}, []byte(`{"contentBlockIndex":0,"delta":{"text":"hi"}}`))

	dec := NewDecoder(bytes.NewReader(frame))
	event, err := dec.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if event.MessageType != "event" || event.EventType != "contentBlockDelta" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if string(event.Data) != `{"contentBlockIndex":0,"delta":{"text":"hi"}}` {
		t.Fatalf("unexpected payload: %s", event.Data)
	}

	if _, err := dec.Next(); err != io.EOF {
		t.Fatalf("expected io.EOF after last frame, got %v", err)
	}
}

func TestDecoder_DecodesExceptionFrame(t *testing.T) {
	frame := buildFrame(t, map[string]string{
		":message-type":   "exception",
		":exception-type": "serviceUnavailableException",
	}, []byte(`{"message":"unavailable"}`))

	dec := NewDecoder(bytes.NewReader(frame))
	event, err := dec.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if event.MessageType != "exception" || event.ExceptionType != "serviceUnavailableException" {
		t.Fatalf("unexpected event: %+v", event)
	}
}

func TestDecoder_MultipleFramesAcrossReads(t *testing.T) {
	frame1 := buildFrame(t, map[string]string{":message-type": "event", ":event-type": "a"}, []byte(`{"n":1}`))
	frame2 := buildFrame(t, map[string]string{":message-type": "event", ":event-type": "b"}, []byte(`{"n":2}`))

	// Simulate a reader that returns partial chunks, like a real network read.
	all := append(append([]byte{}, frame1...), frame2...)
	dec := NewDecoder(&chunkedReader{data: all, chunkSize: 7})

	e1, err := dec.Next()
	if err != nil || e1.EventType != "a" {
		t.Fatalf("first event = %+v, err = %v", e1, err)
	}
	e2, err := dec.Next()
	if err != nil || e2.EventType != "b" {
		t.Fatalf("second event = %+v, err = %v", e2, err)
	}
	if _, err := dec.Next(); err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestDecoder_TruncatedFinalFrameErrors(t *testing.T) {
	frame := buildFrame(t, map[string]string{":message-type": "event", ":event-type": "a"}, []byte(`{"n":1}`))
	truncated := frame[:len(frame)-3]

	dec := NewDecoder(bytes.NewReader(truncated))
	_, err := dec.Next()
	if err == nil {
		t.Fatal("expected error for truncated frame, got nil")
	}
	if !strings.Contains(err.Error(), "Incomplete Amazon Bedrock event-stream frame") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestDecoder_PreludeCRCMismatchErrors(t *testing.T) {
	frame := buildFrame(t, map[string]string{":message-type": "event", ":event-type": "a"}, []byte(`{"n":1}`))
	// Corrupt a byte within the prelude CRC-covered region.
	corrupted := append([]byte{}, frame...)
	corrupted[1] ^= 0xFF

	dec := NewDecoder(bytes.NewReader(corrupted))
	_, err := dec.Next()
	if err == nil {
		t.Fatal("expected CRC mismatch error, got nil")
	}
}

func TestDecoder_MessageCRCMismatchErrors(t *testing.T) {
	frame := buildFrame(t, map[string]string{":message-type": "event", ":event-type": "a"}, []byte(`{"n":1}`))
	corrupted := append([]byte{}, frame...)
	// Corrupt a payload byte (after the prelude+headers), which invalidates the
	// message CRC without touching the prelude CRC.
	corrupted[len(corrupted)-5] ^= 0xFF

	dec := NewDecoder(bytes.NewReader(corrupted))
	_, err := dec.Next()
	if err == nil {
		t.Fatal("expected message CRC mismatch error, got nil")
	}
}

// chunkedReader returns data in small chunks to exercise the decoder's
// incremental buffering.
type chunkedReader struct {
	data      []byte
	chunkSize int
	pos       int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
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
