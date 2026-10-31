package ai

import (
	"context"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// SpeechTextPart is a piece of text to synthesize with ExperimentalStreamSpeech.
// Stream these (or plain strings) to give parts their own voice, for example
// for dialogue.
type SpeechTextPart = provider.SpeechStreamTextPart

type chanSpeechTextStream struct {
	parts    <-chan SpeechTextPart
	onCancel func(error)

	mu        sync.Mutex
	cancelled bool
}

// NewChannelSpeechTextStream creates a provider.SpeechTextStream backed by a
// channel of text parts, for feeding incremental text (for example LLM
// output) to ExperimentalStreamSpeech. The producer closes parts when there
// is no more text. onCancel, if non-nil, runs at most once when the consumer
// cancels the stream (setup failed, the provider failed, or the caller closed
// the result's stream), so the producer can stop.
func NewChannelSpeechTextStream(parts <-chan SpeechTextPart, onCancel func(reason error)) provider.SpeechTextStream {
	return &chanSpeechTextStream{parts: parts, onCancel: onCancel}
}

// NewChannelStringSpeechTextStream is NewChannelSpeechTextStream for a channel
// of plain text deltas.
func NewChannelStringSpeechTextStream(deltas <-chan string, onCancel func(reason error)) provider.SpeechTextStream {
	return &stringSpeechTextStream{deltas: deltas, onCancel: onCancel}
}

func (s *chanSpeechTextStream) Next(ctx context.Context) (*provider.SpeechStreamTextPart, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case part, ok := <-s.parts:
		if !ok {
			return nil, io.EOF
		}
		return &part, nil
	}
}

func (s *chanSpeechTextStream) Cancel(reason error) {
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

type stringSpeechTextStream struct {
	deltas   <-chan string
	onCancel func(error)

	mu        sync.Mutex
	cancelled bool
}

func (s *stringSpeechTextStream) Next(ctx context.Context) (*provider.SpeechStreamTextPart, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case delta, ok := <-s.deltas:
		if !ok {
			return nil, io.EOF
		}
		return &provider.SpeechStreamTextPart{Text: delta}, nil
	}
}

func (s *stringSpeechTextStream) Cancel(reason error) {
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

// singleSpeechTextStream yields one part, then io.EOF (a complete string).
type singleSpeechTextStream struct {
	mu   sync.Mutex
	part provider.SpeechStreamTextPart
	done bool
}

func (s *singleSpeechTextStream) Next(ctx context.Context) (*provider.SpeechStreamTextPart, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil, io.EOF
	}
	s.done = true
	part := s.part
	return &part, nil
}

func (s *singleSpeechTextStream) Cancel(error) {
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
}
