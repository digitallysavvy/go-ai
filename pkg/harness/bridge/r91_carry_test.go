package bridge

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha1" //nolint:gosec // RFC 6455 accept key
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// r91carryFrame is an unmasked server frame (payload <= 125 bytes).
func r91carryFrame(fin bool, opcode byte, payload []byte) []byte {
	b0 := opcode
	if fin {
		b0 |= 0x80
	}
	return append([]byte{b0, byte(len(payload))}, payload...)
}

// r91carryReadClientFrame reads one masked client frame's payload.
func r91carryReadClientFrame(r *bufio.Reader) ([]byte, error) {
	h := make([]byte, 2)
	if _, err := io.ReadFull(r, h); err != nil {
		return nil, err
	}
	n := int(h[1] & 0x7f)
	if n == 126 {
		ext := make([]byte, 2)
		if _, err := io.ReadFull(r, ext); err != nil {
			return nil, err
		}
		n = int(ext[0])<<8 | int(ext[1])
	}
	mask := make([]byte, 4)
	if _, err := io.ReadFull(r, mask); err != nil {
		return nil, err
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return nil, err
	}
	for i := range p {
		p[i] ^= mask[i%4]
	}
	return p, nil
}

// r91carryRawBridge is a WebSocket server that sends the authentication
// challenge split over two frames, checks the proof, then sends a data
// message split over three frames with a ping between them.
func r91carryRawBridge(t *testing.T, pub ed25519.PublicKey, challenge string, proofOK chan<- bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close() //nolint:errcheck
		_ = c.SetDeadline(time.Now().Add(10 * time.Second))
		br := bufio.NewReader(c)
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		h := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) //nolint:gosec
		_, _ = io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+
			base64.StdEncoding.EncodeToString(h[:])+"\r\n\r\n")
		frame := []byte(SerializeBridgeAuthChallengeFrame(challenge))
		_, _ = c.Write(r91carryFrame(false, 0x1, frame[:20]))
		_, _ = c.Write(r91carryFrame(true, 0x0, frame[20:]))
		proof, err := r91carryReadClientFrame(br)
		ok := err == nil && VerifyBridgeAuthProof(pub, challenge, proof, false)
		proofOK <- ok
		if !ok {
			return
		}
		euro := []byte("€")
		_, _ = c.Write(r91carryFrame(false, 0x1, append([]byte(`{"type":"x","v":"`), euro[:1]...)))
		_, _ = c.Write(r91carryFrame(true, 0x9, []byte("p")))
		_, _ = c.Write(r91carryFrame(false, 0x0, euro[1:]))
		_, _ = c.Write(r91carryFrame(true, 0x0, []byte(`"}`)))
		_, _ = c.Write(r91carryFrame(true, 0x8, []byte{0x03, 0xe8}))
		_, _ = io.Copy(io.Discard, br)
	}()
	return "ws://" + ln.Addr().String() + "/"
}

// core2-02 (bridge site): the challenge and later messages are whole
// WebSocket messages even when the bridge fragments them (TS `ws` message
// events, RFC 6455 5.4).
func TestR91carryBridgeReassemblesFragmentedMessages(t *testing.T) {
	keys := CreateBridgeAuthKeyPair()
	pub, err := ParseBridgeAuthPublicKey(keys.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	proofOK := make(chan bool, 1)
	url := r91carryRawBridge(t, pub, NewBridgeAuthChallenge(), proofOK)
	conn, err := Dial(context.Background(), harness.PortEndpoint{URL: url}, DialOptions{AuthPrivateKey: keys.PrivateKey, HelloTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close() //nolint:errcheck
	if !<-proofOK {
		t.Fatal("bridge rejected the proof")
	}
	msg, err := conn.Receive()
	if err != nil || string(msg) != `{"type":"x","v":"€"}` {
		t.Fatalf("Receive = %q, %v", msg, err)
	}
	if _, err := conn.Receive(); err == nil {
		t.Fatal("want the close")
	}
}
