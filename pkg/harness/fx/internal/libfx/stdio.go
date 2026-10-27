package libfx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"
)

// MaxCoreMessageBytes caps one newline-terminated message from the core
// (core-output.js maxCoreMessageBytes).
const MaxCoreMessageBytes = 64 << 20

var errStdinClosed = errors.New("fx runtime stdin is closed")

// byteQueue is the core's stdin (fx-sdk.js ByteQueue). Pushes never block,
// which removes the deadlock of a guest blocked in fd_write while the host
// is blocked writing stdin. Waiters are woken by a push or by close.
type byteQueue struct {
	mu     sync.Mutex
	chunks [][]byte
	closed bool
	wake   chan struct{} // closed and replaced on every push or close
}

func newByteQueue() *byteQueue { return &byteQueue{wake: make(chan struct{})} }

func (q *byteQueue) push(data []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return errStdinClosed
	}
	if len(data) == 0 {
		return nil
	}
	q.chunks = append(q.chunks, bytes.Clone(data))
	q.signalLocked()
	return nil
}

func (q *byteQueue) signalLocked() {
	close(q.wake)
	q.wake = make(chan struct{})
}

func (q *byteQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	q.signalLocked()
}

// read takes up to max bytes from the head chunk; nil when empty.
func (q *byteQueue) read(max int) []byte {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.chunks) == 0 || max <= 0 {
		return nil
	}
	chunk := q.chunks[0]
	if len(chunk) <= max {
		q.chunks = q.chunks[1:]
		return chunk
	}
	q.chunks[0] = chunk[max:]
	return chunk[:max]
}

func (q *byteQueue) hasData() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.chunks) > 0
}

func (q *byteQueue) isClosed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed
}

// wait blocks until a push or close, the timeout (negative: none), or ctx.
// It reports whether it was woken (true) rather than timed out. A closed
// queue returns true at once, as ByteQueue.wait does.
func (q *byteQueue) wait(ctx context.Context, timeout time.Duration) bool {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return true
	}
	wake := q.wake
	q.mu.Unlock()
	var timer <-chan time.Time
	if timeout >= 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-wake:
		return true
	case <-timer:
		return false
	case <-ctx.Done():
		return true
	}
}

// coreOutput splits the core's stdout into newline-terminated UTF-8 JSON
// messages (core-output.js CoreOutput). handle runs for each non-empty
// line, on the guest goroutine; when it blocks, fd_write blocks, which is
// the backpressure the JS host gets from a pending handler.
type coreOutput struct {
	buf    []byte
	handle func(line []byte, size int) error
	closed bool
}

func (o *coreOutput) write(chunk []byte) error {
	for len(chunk) > 0 {
		if o.closed {
			return errors.New("core output is closed")
		}
		nl := bytes.IndexByte(chunk, '\n')
		fragment := chunk
		if nl >= 0 {
			fragment = chunk[:nl]
		}
		length := len(o.buf) + len(fragment)
		extra := 0
		if nl >= 0 {
			extra = 1
		}
		if length+extra > MaxCoreMessageBytes {
			return errors.New("core output message exceeds 64 MiB")
		}
		if nl < 0 {
			o.buf = append(o.buf, fragment...)
			return nil
		}
		line := fragment
		if len(o.buf) > 0 {
			o.buf = append(o.buf, fragment...)
			line = o.buf
		}
		chunk = chunk[nl+1:]
		if !utf8.Valid(line) {
			return errors.New("core output is not valid UTF-8")
		}
		if len(line) > 0 {
			// handle must not retain line.
			if err := o.handle(line, length+1); err != nil {
				return err
			}
		}
		o.buf = o.buf[:0]
		if cap(o.buf) > 1<<20 {
			o.buf = nil
		}
	}
	return nil
}

func (o *coreOutput) finish() error {
	if len(o.buf) > 0 {
		return errors.New("core output ended within a message")
	}
	return nil
}

// tailBuffer keeps the last max bytes written, for diagnostics.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

func (t *tailBuffer) describe() string {
	s := t.String()
	if s == "" {
		return ""
	}
	return fmt.Sprintf(" (stderr: %q)", s)
}
