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
	buf   *chunkBuffer
	index int
}

// Next returns the next buffered chunk, blocking until one is available or
// the buffer is closed.
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
// other readers keep working.
func (r *chunkBufferReader) Close() error {
	r.buf.mu.Lock()
	r.index = len(r.buf.chunks)
	r.buf.mu.Unlock()
	return nil
}
