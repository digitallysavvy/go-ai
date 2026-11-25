package websocket

import (
	"errors"
	"io"
	"unicode/utf8"

	"golang.org/x/net/websocket"
)

// ErrMessageTooLarge is the ReceiveMessage error for a message whose
// reassembled payload exceeds the connection's MaxPayloadBytes
// (websocket.DefaultMaxPayloadBytes when zero). The connection is failed
// with close status 1009.
var ErrMessageTooLarge = errors.New("websocket: message too large")

// ErrFragmentation is the ReceiveMessage error for frames that break the
// RFC 6455 fragmentation rules: a continuation frame with no message in
// progress, a new text or binary frame before the previous message's final
// fragment, or a fragmented control frame. The connection is failed with
// close status 1002.
var ErrFragmentation = errors.New("websocket: invalid frame fragmentation")

// ErrInvalidOpcode is the ReceiveMessage error for a frame with a reserved
// opcode. The connection is failed with close status 1002.
var ErrInvalidOpcode = errors.New("websocket: invalid opcode")

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
)

// ReceiveMessage reads one whole data message from conn: a text or binary
// frame and its continuation frames up to the one with the FIN bit, joined
// into one payload, as a browser or Node WebSocket delivers it (TS
// connectToWebSocket's message event). golang.org/x/net/websocket's
// Message.Receive and Codec.Receive return a single frame, so a fragmented
// message would otherwise arrive as several messages and a UTF-8 sequence
// split across fragments would fail the text check.
//
// Control frames between fragments are handled as x/net does: a ping is
// answered with a pong, a pong is skipped, and a close frame ends the read
// with io.EOF. A text message that is not valid UTF-8 as a whole fails the
// connection (close 1007, ErrInvalidUTF8Text); it is returned unchanged
// otherwise, without BOM or replacement handling. binary reports a binary
// message.
//
// ReceiveMessage must not run concurrently with another receive on the same
// connection (Message.Receive, Codec.Receive or Conn.Read).
func ReceiveMessage(conn *websocket.Conn) (data []byte, binary bool, err error) {
	limit := conn.MaxPayloadBytes
	if limit <= 0 {
		limit = websocket.DefaultMaxPayloadBytes
	}
	started := false
	for {
		f, err := conn.NewFrameReader()
		if err != nil {
			return nil, false, err
		}
		// The raw header gives the FIN bit and the opcode before
		// HandleFrame rewrites a continuation frame's opcode.
		var header []byte
		if hr := f.HeaderReader(); hr != nil {
			if header, err = io.ReadAll(hr); err != nil {
				return nil, false, err
			}
		}
		if len(header) < 2 {
			return nil, false, ErrFragmentation
		}
		fin := header[0]&0x80 != 0
		opcode := header[0] & 0x0f
		payloadLen := f.Len() - len(header)

		if opcode >= 0x8 && !fin {
			return nil, false, failConn(conn, 1002, ErrFragmentation)
		}
		switch opcode {
		case opContinuation:
			if !started {
				return nil, false, failConn(conn, 1002, ErrFragmentation)
			}
		case opText, opBinary:
			if started {
				return nil, false, failConn(conn, 1002, ErrFragmentation)
			}
		case websocket.CloseFrame, websocket.PingFrame, websocket.PongFrame:
		default:
			// A reserved opcode fails the connection (RFC 6455 5.2), as
			// Node's WebSocket does, instead of being returned as data.
			return nil, false, failConn(conn, 1002, ErrInvalidOpcode)
		}

		handled, err := conn.HandleFrame(f)
		if err != nil {
			return nil, false, err
		}
		if handled == nil {
			continue // ping or pong
		}
		if payloadLen < 0 || payloadLen > limit-len(data) {
			return nil, false, failConn(conn, 1009, ErrMessageTooLarge)
		}
		if !started {
			started = true
			binary = opcode == opBinary
			data = make([]byte, 0, payloadLen)
		}
		chunk, err := io.ReadAll(handled)
		if err != nil {
			return nil, false, err
		}
		data = append(data, chunk...)
		if fin {
			break
		}
	}
	if !binary && !utf8.Valid(data) {
		return nil, false, failConn(conn, 1007, ErrInvalidUTF8Text)
	}
	return data, binary, nil
}

// failConn sends a close frame with status code and err's text as the
// reason, and returns err.
func failConn(conn *websocket.Conn, code int, err error) error {
	reason := err.Error()
	if len(reason) > 123 {
		reason = reason[:123]
	}
	_ = frameCodec.Send(conn, append([]byte{byte(code >> 8), byte(code)}, reason...))
	return err
}
