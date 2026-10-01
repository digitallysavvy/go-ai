// Package eventstream decodes the AWS event-stream binary framing format used
// by Amazon Bedrock's Converse streaming API (application/vnd.amazon.eventstream).
//
// Frame format (all integers big-endian):
//
//	[total_length:4][headers_length:4][prelude_crc:4][headers][payload][message_crc:4]
//
// Ported from the TS SDK's amazon-bedrock-event-stream-decoder.ts, which wraps
// @smithy/eventstream-codec.
package eventstream

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

// Event is a single decoded event-stream message.
type Event struct {
	// MessageType is the value of the ":message-type" header, typically
	// "event" or "exception".
	MessageType string

	// EventType is the value of the ":event-type" header, present when
	// MessageType == "event".
	EventType string

	// ExceptionType is the value of the ":exception-type" header, present
	// when MessageType == "exception".
	ExceptionType string

	// Data is the raw message payload (JSON-encoded).
	Data []byte
}

// Decoder incrementally decodes AWS event-stream frames from a reader.
type Decoder struct {
	r   io.Reader
	buf []byte
}

// NewDecoder creates a new event-stream Decoder that reads frames from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

// Next reads and decodes the next frame from the stream.
//
// It returns io.EOF when the stream ends cleanly (no buffered bytes
// remaining). If bytes remain buffered but do not form a complete frame when
// the underlying reader reaches EOF, Next returns an error describing the
// truncation (mirrors the TS SDK's flush() error). Frame corruption (CRC
// mismatches, malformed headers) is always surfaced as an error rather than
// silently dropped.
func (d *Decoder) Next() (*Event, error) {
	for {
		if len(d.buf) >= 4 {
			totalLength := binary.BigEndian.Uint32(d.buf[0:4])
			if totalLength < 16 {
				return nil, fmt.Errorf("amazon bedrock event-stream: invalid frame length %d", totalLength)
			}
			// Compare in uint64 so a totalLength near the uint32 max can
			// never wrap around when compared against the buffered length.
			if uint64(len(d.buf)) >= uint64(totalLength) {
				frame := d.buf[:totalLength]
				d.buf = d.buf[totalLength:]
				return decodeFrame(frame)
			}
		}

		chunk := make([]byte, 32*1024)
		n, err := d.r.Read(chunk)
		if n > 0 {
			d.buf = append(d.buf, chunk[:n]...)
			continue
		}
		if err != nil {
			if err == io.EOF {
				if len(d.buf) > 0 {
					return nil, fmt.Errorf("Incomplete Amazon Bedrock event-stream frame: %d buffered bytes remain at end of stream.", len(d.buf))
				}
				return nil, io.EOF
			}
			return nil, err
		}
	}
}

func decodeFrame(frame []byte) (*Event, error) {
	// Prelude: total_length(4) + headers_length(4) + prelude_crc(4).
	if len(frame) < 12 {
		return nil, fmt.Errorf("amazon bedrock event-stream: frame too short for prelude: %d bytes", len(frame))
	}
	totalLength := binary.BigEndian.Uint32(frame[0:4])
	headersLength := binary.BigEndian.Uint32(frame[4:8])
	preludeCRC := binary.BigEndian.Uint32(frame[8:12])

	calculatedPreludeCRC := crc32.ChecksumIEEE(frame[0:8])
	if calculatedPreludeCRC != preludeCRC {
		return nil, fmt.Errorf("amazon bedrock event-stream: prelude CRC mismatch: expected %d, got %d", preludeCRC, calculatedPreludeCRC)
	}

	// Compare lengths in int64 (len(frame) is a valid int, totalLength and
	// headersLength are uint32 <= 2^32-1): no combination of these values
	// can overflow int64, unlike the original uint32 arithmetic
	// (12+headersLength+4), which could wrap to a small/zero value for a
	// crafted headersLength near the uint32 max and let an out-of-range
	// headersEnd slip through to the slice expression below.
	frameLen64 := int64(len(frame))
	if frameLen64 != int64(totalLength) {
		return nil, fmt.Errorf("amazon bedrock event-stream: frame length mismatch: header says %d, got %d", totalLength, len(frame))
	}
	if int64(totalLength) < int64(headersLength)+16 {
		return nil, fmt.Errorf("amazon bedrock event-stream: headers length %d exceeds frame", headersLength)
	}

	// headersLength is now known to be <= totalLength-16 == len(frame)-16,
	// so converting it to int and adding 12 cannot overflow or exceed
	// len(frame).
	headersEnd := 12 + int(headersLength)
	headersBytes := frame[12:headersEnd]
	payload := frame[headersEnd : totalLength-4]
	messageCRC := binary.BigEndian.Uint32(frame[totalLength-4 : totalLength])

	calculatedMessageCRC := crc32.ChecksumIEEE(frame[0 : totalLength-4])
	if calculatedMessageCRC != messageCRC {
		return nil, fmt.Errorf("amazon bedrock event-stream: message CRC mismatch: expected %d, got %d", messageCRC, calculatedMessageCRC)
	}

	headers, err := parseHeaders(headersBytes)
	if err != nil {
		return nil, fmt.Errorf("amazon bedrock event-stream: failed to parse headers: %w", err)
	}

	// Copy the payload so callers holding onto Event.Data are not affected by
	// buffer reuse in future reads.
	data := make([]byte, len(payload))
	copy(data, payload)

	return &Event{
		MessageType:   headers[":message-type"],
		EventType:     headers[":event-type"],
		ExceptionType: headers[":exception-type"],
		Data:          data,
	}, nil
}

// parseHeaders parses AWS event-stream headers.
// Header format: [name_length:1][name][value_type:1][value...]
func parseHeaders(data []byte) (map[string]string, error) {
	headers := make(map[string]string)
	i := 0
	for i < len(data) {
		if i+1 > len(data) {
			return nil, fmt.Errorf("truncated header name length")
		}
		nameLength := int(data[i])
		i++
		if i+nameLength > len(data) {
			return nil, fmt.Errorf("truncated header name")
		}
		name := string(data[i : i+nameLength])
		i += nameLength

		if i+1 > len(data) {
			return nil, fmt.Errorf("truncated header value type")
		}
		valueType := data[i]
		i++

		switch valueType {
		case 0: // bool true
			headers[name] = "true"
		case 1: // bool false
			headers[name] = "false"
		case 2: // byte
			if i+1 > len(data) {
				return nil, fmt.Errorf("truncated byte header value")
			}
			headers[name] = fmt.Sprintf("%d", int8(data[i]))
			i++
		case 3: // short
			if i+2 > len(data) {
				return nil, fmt.Errorf("truncated short header value")
			}
			headers[name] = fmt.Sprintf("%d", int16(binary.BigEndian.Uint16(data[i:i+2])))
			i += 2
		case 4: // int
			if i+4 > len(data) {
				return nil, fmt.Errorf("truncated int header value")
			}
			headers[name] = fmt.Sprintf("%d", int32(binary.BigEndian.Uint32(data[i:i+4])))
			i += 4
		case 5: // long
			if i+8 > len(data) {
				return nil, fmt.Errorf("truncated long header value")
			}
			headers[name] = fmt.Sprintf("%d", int64(binary.BigEndian.Uint64(data[i:i+8])))
			i += 8
		case 6: // byte array
			if i+2 > len(data) {
				return nil, fmt.Errorf("truncated byte-array header length")
			}
			valueLength := int(binary.BigEndian.Uint16(data[i : i+2]))
			i += 2
			if i+valueLength > len(data) {
				return nil, fmt.Errorf("truncated byte-array header value")
			}
			headers[name] = string(data[i : i+valueLength])
			i += valueLength
		case 7: // string
			if i+2 > len(data) {
				return nil, fmt.Errorf("truncated string header length")
			}
			valueLength := int(binary.BigEndian.Uint16(data[i : i+2]))
			i += 2
			if i+valueLength > len(data) {
				return nil, fmt.Errorf("truncated string header value")
			}
			headers[name] = string(data[i : i+valueLength])
			i += valueLength
		case 8: // timestamp (8-byte long, epoch millis)
			if i+8 > len(data) {
				return nil, fmt.Errorf("truncated timestamp header value")
			}
			headers[name] = fmt.Sprintf("%d", int64(binary.BigEndian.Uint64(data[i:i+8])))
			i += 8
		case 9: // uuid (16 bytes)
			if i+16 > len(data) {
				return nil, fmt.Errorf("truncated uuid header value")
			}
			headers[name] = fmt.Sprintf("%x", data[i:i+16])
			i += 16
		default:
			return nil, fmt.Errorf("unsupported header value type: %d", valueType)
		}
	}
	return headers, nil
}
