package bedrock

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
)

// buildBedrockEventFrame constructs a single valid AWS event-stream binary
// frame, mirroring the wire format the real Bedrock Converse-stream endpoint
// emits. Used to build httptest fixtures for streaming tests without
// depending on a recorded network capture.
func buildBedrockEventFrame(headers map[string]string, payload []byte) []byte {
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

	return buf.Bytes()
}

// buildBedrockEventStreamBody concatenates several "event"-typed frames, one
// per (eventType, jsonPayload) pair, into a single response body.
func buildBedrockEventStreamBody(events [][2]string) []byte {
	var out bytes.Buffer
	for _, e := range events {
		frame := buildBedrockEventFrame(map[string]string{
			":message-type": "event",
			":event-type":   e[0],
		}, []byte(e[1]))
		out.Write(frame)
	}
	return out.Bytes()
}
