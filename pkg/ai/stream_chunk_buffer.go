package ai

import (
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// chunkBuffer records every chunk produced by processStream's multi-step loop
// and lets any number of independent readers replay the full sequence from
// the beginning, similar to the tee()'d fullStream ReadableStream in the
// TypeScript SDK. This decouples Stream()/Chunks()/ReadAll() from the raw
// per-step provider stream that processStream consumes directly: without
// this indirection, calling Stream() while processStream is running would
// race with processStream's own reads of the same underlying stream.
//
// There is exactly one writer (the processStream goroutine) and any number
// of readers. Chunks are never removed, so readers may lag arbitrarily far
// behind the writer.
type chunkBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	chunks []provider.StreamChunk
	closed bool
	err    error
}

func newChunkBuffer() *chunkBuffer {
	b := &chunkBuffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// push appends a chunk and wakes any blocked readers.
func (b *chunkBuffer) push(c provider.StreamChunk) {
	b.mu.Lock()
	b.chunks = append(b.chunks, c)
	b.cond.Broadcast()
	b.mu.Unlock()
}

// close marks the buffer as complete. err (if non-nil and not io.EOF) is
// returned by readers once they exhaust the buffered chunks.
func (b *chunkBuffer) close(err error) {
	b.mu.Lock()
	b.closed = true
	b.err = err
	b.cond.Broadcast()
	b.mu.Unlock()
}

// reader returns a new independent cursor over the buffer, starting at the
// first chunk. Multiple readers may be created and consumed concurrently.
func (b *chunkBuffer) reader() *chunkBufferReader {
	return &chunkBufferReader{buf: b}
}

// chunkBufferReader implements provider.TextStream over a chunkBuffer.
type chunkBufferReader struct {
	buf    *chunkBuffer
	index  int
	closed bool
}

// Next returns the next buffered chunk, blocking until one is available, the
// shared buffer is closed, or this reader's own Close is called. A
// concurrent Close (e.g. a consumer cancelling a blocked read from another
// goroutine) must wake a Next already waiting in cond.Wait below -- Close
// broadcasts on the same cond for exactly this reason (R1-4). Close also
// advances index to len(chunks) (see Close below), so a closed reader never
// returns a chunk again even if it had unread ones buffered; Next simply
// reports io.EOF once closed, the same terminal value a reader sees once the
// whole shared buffer closes cleanly.
func (r *chunkBufferReader) Next() (*provider.StreamChunk, error) {
	r.buf.mu.Lock()
	defer r.buf.mu.Unlock()
	for {
		if r.index < len(r.buf.chunks) {
			c := r.buf.chunks[r.index]
			r.index++
			return &c, nil
		}
		if r.buf.closed {
			if r.buf.err != nil {
				return nil, r.buf.err
			}
			return nil, io.EOF
		}
		if r.closed {
			return nil, io.EOF
		}
		r.buf.cond.Wait()
	}
}

// Err returns the terminal error of the underlying buffer, if any.
func (r *chunkBufferReader) Err() error {
	r.buf.mu.Lock()
	defer r.buf.mu.Unlock()
	if r.buf.err != nil && r.buf.err != io.EOF {
		return r.buf.err
	}
	return nil
}

// Close detaches this reader. The shared buffer itself is not affected, so
// other readers keep working. Broadcasting on the shared cond wakes any
// goroutine currently blocked in this reader's Next (or any other reader's
// Next -- each re-checks its own closed/index state and simply loops back to
// sleep if it still has nothing to do), so a concurrent Close always causes
// a blocked Next to return promptly instead of waiting for the next push or
// buffer-wide close (R1-4).
func (r *chunkBufferReader) Close() error {
	r.buf.mu.Lock()
	r.closed = true
	r.index = len(r.buf.chunks)
	r.buf.cond.Broadcast()
	r.buf.mu.Unlock()
	return nil
}
