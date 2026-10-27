package bridge

import (
	"net"
	"sync/atomic"
)

// closeSniffer wraps the raw connection under a client WebSocket and records
// the status code of the first close frame the server sends.
// golang.org/x/net/websocket discards close payloads and reports every close
// as io.EOF, but the bridge rejects a bad token with code 1008, which the
// host must tell apart from a transient drop (TS fails fast on 1008 and
// retries everything else).
//
// It only watches the byte stream; nothing is buffered beyond the two code
// bytes, so a hostile peer cannot make it grow. Server frames are unmasked
// (RFC 6455), and masked ones are still skipped correctly.
type closeSniffer struct {
	net.Conn
	code atomic.Int32 // 0 until a close frame with a status code was seen

	// Parser state; only touched by Read, which x/net calls from one reader
	// at a time.
	inHeaders  bool
	tail       uint32 // last four bytes while skipping the HTTP response
	hdr        [14]byte
	hdrLen     int
	hdrNeed    int
	remaining  uint64
	opcode     byte
	masked     bool
	payloadPos int
	codeBytes  [2]byte
	done       bool
}

func newCloseSniffer(c net.Conn) *closeSniffer {
	return &closeSniffer{Conn: c, inHeaders: true}
}

// CloseCode returns the status code of the server's close frame, or 0.
func (s *closeSniffer) CloseCode() int { return int(s.code.Load()) }

func (s *closeSniffer) Read(p []byte) (int, error) {
	n, err := s.Conn.Read(p)
	if n > 0 && !s.done {
		s.feed(p[:n])
	}
	return n, err
}

func (s *closeSniffer) feed(b []byte) {
	for len(b) > 0 && !s.done {
		if s.inHeaders {
			c := b[0]
			b = b[1:]
			s.tail = s.tail<<8 | uint32(c)
			if s.tail == 0x0d0a0d0a {
				s.inHeaders = false
			}
			continue
		}
		if s.remaining > 0 {
			take := uint64(len(b))
			if take > s.remaining {
				take = s.remaining
			}
			if s.opcode == 8 && !s.masked {
				for i := uint64(0); i < take && s.payloadPos < 2; i++ {
					s.codeBytes[s.payloadPos] = b[i]
					s.payloadPos++
				}
				if s.payloadPos == 2 {
					s.code.Store(int32(s.codeBytes[0])<<8 | int32(s.codeBytes[1]))
					s.done = true
					return
				}
			}
			b = b[take:]
			s.remaining -= take
			continue
		}
		// Collect a frame header: 2 bytes, then extended length and mask key.
		if s.hdrLen < 2 {
			s.hdr[s.hdrLen] = b[0]
			s.hdrLen++
			b = b[1:]
			if s.hdrLen < 2 {
				continue
			}
			s.hdrNeed = 2
			switch s.hdr[1] & 0x7f {
			case 126:
				s.hdrNeed += 2
			case 127:
				s.hdrNeed += 8
			}
			if s.hdr[1]&0x80 != 0 {
				s.hdrNeed += 4
			}
		}
		for s.hdrLen < s.hdrNeed && len(b) > 0 {
			s.hdr[s.hdrLen] = b[0]
			s.hdrLen++
			b = b[1:]
		}
		if s.hdrLen < s.hdrNeed {
			return
		}
		var length uint64
		switch l := s.hdr[1] & 0x7f; l {
		case 126:
			length = uint64(s.hdr[2])<<8 | uint64(s.hdr[3])
		case 127:
			for i := 0; i < 8; i++ {
				length = length<<8 | uint64(s.hdr[2+i])
			}
		default:
			length = uint64(l)
		}
		s.opcode = s.hdr[0] & 0x0f
		s.masked = s.hdr[1]&0x80 != 0
		s.remaining = length
		s.payloadPos = 0
		s.hdrLen = 0
		if s.opcode == 8 && length < 2 {
			s.done = true // close without a status code
		}
	}
}
