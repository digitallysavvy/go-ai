package vercel

import (
	"bytes"
	"io"
	"sync"
)

// streamBuffer is an unbounded, non-blocking single-producer/single-consumer
// byte stream: Write appends and returns immediately (never blocks the
// producer), Read blocks only until data or EOF is available. This mirrors
// JavaScript's `ReadableStream.enqueue`, which buffers rather than applying
// backpressure to the producer — unlike io.Pipe, whose Write blocks until a
// reader drains it. RemoteProcess uses one of these per output stream so a
// caller that fully drains stdout before touching stderr (or ignores one
// stream entirely) cannot deadlock the single goroutine feeding both from one
// ndjson log stream.
type streamBuffer struct {
	mu   sync.Mutex
	cond *sync.Cond
	buf  bytes.Buffer
	err  error // set on Close(err); io.EOF for a clean close
	done bool
}

func newStreamBuffer() *streamBuffer {
	s := &streamBuffer{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Write appends p to the buffer and wakes any blocked reader.
func (s *streamBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	n, err := s.buf.Write(p)
	s.cond.Broadcast()
	s.mu.Unlock()
	return n, err
}

// Close marks the stream finished; io.EOF for a clean end, any other error to
// surface a read failure.
func (s *streamBuffer) Close(err error) {
	if err == nil {
		err = io.EOF
	}
	s.mu.Lock()
	if !s.done {
		s.done = true
		s.err = err
	}
	s.cond.Broadcast()
	s.mu.Unlock()
}

// Read implements io.Reader, blocking until data is available, the stream is
// closed, or both (draining remaining buffered data before returning the
// close error).
func (s *streamBuffer) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.buf.Len() == 0 && !s.done {
		s.cond.Wait()
	}
	if s.buf.Len() > 0 {
		return s.buf.Read(p)
	}
	return 0, s.err
}
