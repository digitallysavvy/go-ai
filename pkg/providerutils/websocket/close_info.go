package websocket

import (
	"net"
	"strconv"
	"sync"
	"unicode/utf8"
)

// CloseInfo watches the raw connection under a client WebSocket and records
// the server's first close frame. golang.org/x/net/websocket discards close
// payloads and reports every close as io.EOF, but TS's onClose receives
// `{ code, reason }` from the transport (the `ws` close event) and several
// providers put them in their "closed before ..." error text.
//
// Like the `ws` close event: a close frame without a status code reports
// 1005, and a connection that ends without any close frame reports 1006.
//
// Only the byte stream is observed; at most 125 payload bytes (the control
// frame limit) are retained, so a hostile peer cannot make it grow.
type CloseInfo struct {
	mu     sync.Mutex
	seen   bool
	code   int
	reason string
}

// Code returns the close status code: the server's, 1005 when its close frame
// carried none, or 1006 when no close frame was seen.
func (c *CloseInfo) Code() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.seen {
		return 1006
	}
	return c.code
}

// Reason returns the close frame's reason text ("" when absent).
func (c *CloseInfo) Reason() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reason
}

// Suffix formats the TS close diagnostics: ` (code N)` or
// ` (code N: reason)`, ready to append before the final period.
func (c *CloseInfo) Suffix() string {
	s := " (code " + strconv.Itoa(c.Code())
	if r := c.Reason(); r != "" {
		s += ": " + r
	}
	return s + ")"
}

func (c *CloseInfo) set(code int, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen {
		return
	}
	c.seen, c.code, c.reason = true, code, reason
}

// Wrap returns the sniffing connection to pass as DialOptions.WrapConn's
// result.
func (c *CloseInfo) Wrap(conn net.Conn) net.Conn {
	return &closeSniffConn{Conn: conn, info: c, inHeaders: true}
}

type closeSniffConn struct {
	net.Conn
	info *CloseInfo

	// Parser state; only touched by Read (one reader at a time).
	inHeaders bool
	tail      uint32
	hdr       [14]byte
	hdrLen    int
	hdrNeed   int
	remaining uint64
	opcode    byte
	masked    bool
	payload   []byte
	done      bool
}

func (s *closeSniffConn) Read(p []byte) (int, error) {
	n, err := s.Conn.Read(p)
	if n > 0 && !s.done {
		s.feed(p[:n])
	}
	return n, err
}

func (s *closeSniffConn) finish() {
	s.done = true
	code, reason := 1005, ""
	if len(s.payload) >= 2 {
		code = int(s.payload[0])<<8 | int(s.payload[1])
		if r := s.payload[2:]; utf8.Valid(r) {
			reason = string(r)
		}
	}
	s.info.set(code, reason)
}

func (s *closeSniffConn) feed(b []byte) {
	for len(b) > 0 && !s.done {
		if s.inHeaders {
			s.tail = s.tail<<8 | uint32(b[0])
			b = b[1:]
			if s.tail == 0x0d0a0d0a {
				s.inHeaders = false
			}
			continue
		}
		if s.remaining > 0 {
			take := min(uint64(len(b)), s.remaining)
			if s.opcode == 8 && !s.masked && len(s.payload) < 125 {
				s.payload = append(s.payload, b[:min(take, uint64(125-len(s.payload)))]...)
			}
			b = b[take:]
			s.remaining -= take
			if s.remaining == 0 && s.opcode == 8 {
				s.finish()
			}
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
		s.payload = s.payload[:0]
		s.hdrLen = 0
		if s.opcode == 8 && length == 0 {
			s.finish()
		}
	}
}
