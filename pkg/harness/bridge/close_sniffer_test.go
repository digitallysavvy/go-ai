package bridge

import (
	"net"
	"testing"
)

func TestCloseSniffer_ParsesCloseCodeAcrossReads(t *testing.T) {
	stream := []byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n\r\n")
	// A text frame with a 126-extended length, a ping, then the close frame.
	text := append([]byte{0x81, 126, 0x00, 0x82}, make([]byte, 130)...)
	stream = append(stream, text...)
	stream = append(stream, 0x89, 0x00)
	stream = append(stream, 0x88, 14, 0x03, 0xf0)
	stream = append(stream, []byte("unauthorized")...)

	for _, chunk := range []int{1, 3, 7, len(stream)} {
		s := newCloseSniffer(nil)
		for i := 0; i < len(stream); i += chunk {
			s.feed(stream[i:min(i+chunk, len(stream))])
		}
		if got := s.CloseCode(); got != 1008 {
			t.Fatalf("chunk %d: code = %d, want 1008", chunk, got)
		}
	}
}

func TestCloseSniffer_NoCodeWithoutCloseFrame(t *testing.T) {
	s := newCloseSniffer(nil)
	s.feed([]byte("HTTP/1.1 101 x\r\n\r\n"))
	s.feed([]byte{0x81, 0x02, 'h', 'i'})
	s.feed([]byte{0x88, 0x00})
	if got := s.CloseCode(); got != 0 {
		t.Fatalf("code = %d, want 0", got)
	}
}

var _ net.Conn = (*closeSniffer)(nil)
