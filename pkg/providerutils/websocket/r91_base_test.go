package websocket

import (
	"context"
	"errors"
	"io"
	"testing"

	"golang.org/x/net/websocket"
)

// r91frame is an unmasked server frame.
func r91frame(fin bool, opcode byte, payload []byte) []byte {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	if len(payload) > 125 {
		panic("test frame too long")
	}
	return append([]byte{b0, byte(len(payload))}, payload...)
}

func r91dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, err := Dial(context.Background(), url, DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// B5 (core2-02): a fragmented text message, with a ping between fragments
// and a UTF-8 sequence split across fragments, is one message.
func TestR91baseReceiveMessageReassemblesFragments(t *testing.T) {
	euro := []byte("€") // e2 82 ac
	url := rawCloseServer(t,
		r91frame(false, opText, append([]byte("price "), euro[:1]...)),
		r91frame(true, websocket.PingFrame, []byte("p")),
		r91frame(false, opContinuation, euro[1:2]),
		r91frame(true, opContinuation, append(euro[2:], " 5"...)),
		r91frame(false, opBinary, []byte{0xff}),
		r91frame(true, opContinuation, []byte{0x00}),
		r91frame(true, opText, []byte("next")),
		closeFrame(1000, ""),
	)
	conn := r91dial(t, url)

	data, binary, err := ReceiveMessage(conn)
	if err != nil || binary || string(data) != "price € 5" {
		t.Fatalf("message 1 = %q binary=%v err=%v", data, binary, err)
	}
	data, binary, err = ReceiveMessage(conn)
	if err != nil || !binary || string(data) != "\xff\x00" {
		t.Fatalf("message 2 = %q binary=%v err=%v", data, binary, err)
	}
	data, binary, err = ReceiveMessage(conn)
	if err != nil || binary || string(data) != "next" {
		t.Fatalf("message 3 = %q binary=%v err=%v", data, binary, err)
	}
	if _, _, err = ReceiveMessage(conn); !errors.Is(err, io.EOF) {
		t.Fatalf("close frame: err = %v, want io.EOF", err)
	}
}

func TestR91baseReceiveMessageFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		frames [][]byte
		want   error
	}{
		{"invalid utf-8 across fragments", [][]byte{r91frame(false, opText, []byte{0xe2}), r91frame(true, opContinuation, []byte("x"))}, ErrInvalidUTF8Text},
		{"continuation without start", [][]byte{r91frame(true, opContinuation, []byte("x"))}, ErrFragmentation},
		{"new message mid-fragment", [][]byte{r91frame(false, opText, []byte("a")), r91frame(true, opText, []byte("b"))}, ErrFragmentation},
		{"fragmented control frame", [][]byte{r91frame(false, websocket.PingFrame, nil)}, ErrFragmentation},
		{"reserved opcode", [][]byte{r91frame(true, 0x3, []byte("x"))}, ErrInvalidOpcode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := r91dial(t, rawCloseServer(t, tc.frames...))
			if _, _, err := ReceiveMessage(conn); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("message too large", func(t *testing.T) {
		conn := r91dial(t, rawCloseServer(t,
			r91frame(false, opText, []byte("abc")),
			r91frame(true, opContinuation, []byte("def")),
		))
		conn.MaxPayloadBytes = 5
		if _, _, err := ReceiveMessage(conn); !errors.Is(err, ErrMessageTooLarge) {
			t.Fatalf("err = %v", err)
		}
	})
}
