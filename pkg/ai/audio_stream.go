package ai

import (
	"context"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// chanAudioStream adapts a Go channel of raw audio chunks to
// provider.AudioStream.
type chanAudioStream struct {
	chunks   <-chan []byte
	onCancel func(error)

	mu        sync.Mutex
	cancelled bool
}

// NewChannelAudioStream creates a provider.AudioStream backed by a channel of
// raw audio chunks, for feeding live/incremental audio (e.g. a microphone) to
// ExperimentalStreamTranscribe / ExperimentalStreamTranslate.
//
// The producer should close chunks when there is no more audio; Next then
// returns io.EOF. onCancel, if non-nil, is invoked at most once when the
// consumer cancels the stream — e.g. because the model's DoStream setup
// failed, or the caller cancelled the result's FullStream — mirroring the TS
// SDK's `audio.cancel(reason)`.
func NewChannelAudioStream(chunks <-chan []byte, onCancel func(reason error)) provider.AudioStream {
	return &chanAudioStream{chunks: chunks, onCancel: onCancel}
}

func (s *chanAudioStream) Next(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case chunk, ok := <-s.chunks:
		if !ok {
			return nil, io.EOF
		}
		return chunk, nil
	}
}

func (s *chanAudioStream) Cancel(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelled {
		return
	}
	s.cancelled = true
	if s.onCancel != nil {
		s.onCancel(reason)
	}
}

// readerAudioStream adapts an io.Reader to provider.AudioStream by reading
// fixed-size chunks.
type readerAudioStream struct {
	r         io.Reader
	chunkSize int

	mu        sync.Mutex
	cancelled bool
}

// NewReaderAudioStream creates a provider.AudioStream that reads raw audio in
// chunkSize-byte chunks from r (e.g. an audio file or a pipe). chunkSize
// defaults to 4096 when <= 0. Cancel closes r when it implements io.Closer.
func NewReaderAudioStream(r io.Reader, chunkSize int) provider.AudioStream {
	if chunkSize <= 0 {
		chunkSize = 4096
	}
	return &readerAudioStream{r: r, chunkSize: chunkSize}
}

func (s *readerAudioStream) Next(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	cancelled := s.cancelled
	s.mu.Unlock()
	if cancelled {
		return nil, io.EOF
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buf := make([]byte, s.chunkSize)
	n, err := s.r.Read(buf)
	if n > 0 {
		return buf[:n], nil
	}
	if err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (s *readerAudioStream) Cancel(reason error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelled {
		return
	}
	s.cancelled = true
	if closer, ok := s.r.(io.Closer); ok {
		_ = closer.Close()
	}
}
