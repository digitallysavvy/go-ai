package ai

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestChunkBuffer_PushNeverBlocksOnSlowReader guards the invariant documented
// on chunkBuffer: push() must never block on a reader, no matter how far
// behind that reader is. This is what lets processStream's writer goroutine
// keep running to completion even if the consumer stops calling Stream()
// (or never starts), which matters now that StreamText always runs
// processStream in the background regardless of whether anyone consumes it.
func TestChunkBuffer_PushNeverBlocksOnSlowReader(t *testing.T) {
	t.Parallel()

	buf := newChunkBuffer()
	reader := buf.reader()
	_ = reader // never advanced — simulates an abandoned consumer

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			buf.push(provider.StreamChunk{Type: provider.ChunkTypeText, Text: "x"})
		}
		buf.close(nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("push()/close() blocked on an unconsumed reader")
	}
}

// TestChunkBuffer_MultipleReadersSeeFullSequence mirrors the tee()'d
// fullStream semantics: every reader created (even after some chunks were
// already pushed, as long as it starts at the beginning) sees the full,
// independent sequence.
func TestChunkBuffer_MultipleReadersSeeFullSequence(t *testing.T) {
	t.Parallel()

	buf := newChunkBuffer()
	buf.push(provider.StreamChunk{Type: provider.ChunkTypeText, Text: "a"})
	buf.push(provider.StreamChunk{Type: provider.ChunkTypeText, Text: "b"})

	r1 := buf.reader()
	r2 := buf.reader()

	buf.push(provider.StreamChunk{Type: provider.ChunkTypeText, Text: "c"})
	buf.close(nil)

	for _, r := range []*chunkBufferReader{r1, r2} {
		var got []string
		for {
			c, err := r.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			got = append(got, c.Text)
		}
		if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
			t.Fatalf("reader saw %v, want [a b c]", got)
		}
	}
}

// TestChunkBuffer_CloseWithErrorPropagatesToReaders checks that a non-EOF
// terminal error set via close() is surfaced by every reader once the
// buffered chunks are exhausted, and that Err() reports it.
func TestChunkBuffer_CloseWithErrorPropagatesToReaders(t *testing.T) {
	t.Parallel()

	buf := newChunkBuffer()
	buf.push(provider.StreamChunk{Type: provider.ChunkTypeText, Text: "only"})
	wantErr := io.ErrUnexpectedEOF
	buf.close(wantErr)

	r := buf.reader()
	if _, err := r.Next(); err != nil {
		t.Fatalf("first Next() error = %v, want nil", err)
	}
	_, err := r.Next()
	if err != wantErr {
		t.Fatalf("Next() error = %v, want %v", err, wantErr)
	}
	if got := r.Err(); got != wantErr {
		t.Fatalf("Err() = %v, want %v", got, wantErr)
	}
}

// TestChunkBuffer_BlockedReaderWakesOnPush ensures a reader blocked waiting
// for the next chunk is woken by a concurrent push rather than needing to
// poll, and that a second blocked reader is woken independently.
func TestChunkBuffer_BlockedReaderWakesOnPush(t *testing.T) {
	t.Parallel()

	buf := newChunkBuffer()
	r1 := buf.reader()
	r2 := buf.reader()

	var wg sync.WaitGroup
	wg.Add(2)
	results := make([]*provider.StreamChunk, 2)
	go func() {
		defer wg.Done()
		c, err := r1.Next()
		if err != nil {
			t.Errorf("r1.Next() error = %v", err)
			return
		}
		results[0] = c
	}()
	go func() {
		defer wg.Done()
		c, err := r2.Next()
		if err != nil {
			t.Errorf("r2.Next() error = %v", err)
			return
		}
		results[1] = c
	}()

	// Give both readers a chance to block in cond.Wait() before pushing.
	time.Sleep(20 * time.Millisecond)
	buf.push(provider.StreamChunk{Type: provider.ChunkTypeText, Text: "woken"})

	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked readers were not woken by push()")
	}
	for i, c := range results {
		if c == nil || c.Text != "woken" {
			t.Fatalf("reader %d got %+v, want {Text: woken}", i, c)
		}
	}
}

// TestChunkBufferReader_CloseDetachesWithoutAffectingOthers checks that
// closing one reader's cursor doesn't disturb another reader over the same
// buffer, matching the doc comment on chunkBuffer.reader().
func TestChunkBufferReader_CloseDetachesWithoutAffectingOthers(t *testing.T) {
	t.Parallel()

	buf := newChunkBuffer()
	buf.push(provider.StreamChunk{Type: provider.ChunkTypeText, Text: "a"})
	buf.close(nil)

	r1 := buf.reader()
	r2 := buf.reader()
	if err := r1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := r1.Next(); err != io.EOF {
		t.Fatalf("closed reader Next() error = %v, want io.EOF", err)
	}
	c, err := r2.Next()
	if err != nil || c.Text != "a" {
		t.Fatalf("r2.Next() = %+v, %v, want {a}, nil", c, err)
	}
}
