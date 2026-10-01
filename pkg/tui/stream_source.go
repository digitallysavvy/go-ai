package tui

import (
	"context"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// streamPumpResult is one stream.Next() outcome, handed from the owning pump
// goroutine (see runPump) back to a waiting Next(ctx) caller.
type streamPumpResult struct {
	chunk *provider.StreamChunk
	err   error
}

// StreamRenderSource wraps StreamTextResult for renderer consumption.
//
// Next/Close on the underlying provider.TextStream are serialized through a
// single owning goroutine, started lazily on the first Next call (runPump).
// That goroutine is the only one that ever touches the stream: a cancelled
// ctx or an explicit Close no longer calls stream.Close() directly from the
// caller's goroutine while a stream.Next() call may still be in flight on
// another goroutine (R2-3) -- some provider.TextStream implementations (e.g.
// a raw provider stream's reader) share state between Next and Close with no
// internal locking of their own, and the two running concurrently is a data
// race. Instead, a cancelled ctx/Close asks the pump to stop; the pump closes
// the stream itself only once its own, possibly in-flight, Next() call has
// returned, then exits -- guaranteeing it never leaks and never races.
type StreamRenderSource struct {
	result           *ai.StreamTextResult
	stream           provider.TextStream
	responseMessages []types.Message
	finalStep        types.StepResult
	usage            types.Usage

	mu        sync.Mutex
	started   bool
	closed    bool
	resultCh  chan streamPumpResult
	stopCh    chan struct{}
	stoppedCh chan struct{}
	stopOnce  sync.Once
}

func NewStreamRenderSource(result *ai.StreamTextResult) *StreamRenderSource {
	return &StreamRenderSource{result: result}
}

func NewStreamRenderSourceFromTextStream(stream provider.TextStream, responseMessages []types.Message, finalStep types.StepResult) *StreamRenderSource {
	return &StreamRenderSource{stream: stream, responseMessages: responseMessages, finalStep: finalStep, usage: finalStep.Usage}
}

func (s *StreamRenderSource) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s == nil {
		return nil, io.EOF
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, io.EOF
	}
	if !s.started {
		stream := s.stream
		if stream == nil && s.result != nil {
			stream = s.result.Stream()
		}
		if stream == nil {
			s.mu.Unlock()
			return nil, io.EOF
		}
		s.started = true
		s.resultCh = make(chan streamPumpResult)
		s.stopCh = make(chan struct{})
		s.stoppedCh = make(chan struct{})
		go s.runPump(stream, s.resultCh, s.stopCh, s.stoppedCh)
	}
	resultCh := s.resultCh
	s.mu.Unlock()

	select {
	case <-ctx.Done():
		// Ask the pump to stop after its current (or next) stream.Next()
		// call returns, rather than calling stream.Close() here ourselves:
		// that would race with the pump's in-flight call on the same
		// stream. We return immediately either way -- the pump finishing
		// and closing the stream happens asynchronously in the background.
		s.requestStop()
		return nil, ctx.Err()
	case res, ok := <-resultCh:
		if !ok {
			// The pump stopped (stream ended, or a stop was requested)
			// without ever delivering a result for this call.
			return nil, io.EOF
		}
		return res.chunk, res.err
	}
}

// runPump is the sole owner of stream for the lifetime of this
// StreamRenderSource: it is the only goroutine that ever calls stream.Next()
// or stream.Close(), so the two can never execute concurrently. It keeps
// pumping chunks to resultCh until the stream ends (Next returns an error),
// a stop is requested via stopCh, or a result could not be delivered because
// the caller stopped selecting on resultCh and asked to stop instead. It
// always closes the stream itself exactly once before exiting, so it never
// leaks regardless of why it stopped.
func (s *StreamRenderSource) runPump(stream provider.TextStream, resultCh chan streamPumpResult, stopCh, stoppedCh chan struct{}) {
	defer close(stoppedCh)
	defer close(resultCh)
	defer func() { _ = stream.Close() }()

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		chunk, err := stream.Next()

		select {
		case resultCh <- streamPumpResult{chunk: chunk, err: err}:
		case <-stopCh:
			return
		}
		if err != nil {
			return
		}
	}
}

// requestStop asks the pump (if one has been started) to stop, exactly once.
func (s *StreamRenderSource) requestStop() {
	s.mu.Lock()
	stopCh := s.stopCh
	s.mu.Unlock()
	if stopCh == nil {
		return
	}
	s.stopOnce.Do(func() { close(stopCh) })
}

func (s *StreamRenderSource) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	alreadyClosed := s.closed
	s.closed = true
	started := s.started
	stoppedCh := s.stoppedCh
	directStream := s.stream
	s.mu.Unlock()

	if alreadyClosed {
		if started {
			<-stoppedCh
		}
		return nil
	}

	if started {
		// The pump owns the stream; ask it to stop and wait for it to
		// finish closing the stream itself (runPump always closes
		// stoppedCh, exactly once, right before it returns).
		s.requestStop()
		<-stoppedCh
	} else if directStream != nil {
		// Next was never called, so no pump exists and no Next() call can
		// be in flight: it is safe to close the stream directly here.
		if err := directStream.Close(); err != nil {
			return err
		}
	}

	if s.result == nil {
		return nil
	}
	return s.result.Close()
}

func (s *StreamRenderSource) ResponseMessages() []types.Message {
	if s == nil {
		return nil
	}
	if s.result == nil {
		return append([]types.Message(nil), s.responseMessages...)
	}
	return s.result.ResponseMessages()
}

func (s *StreamRenderSource) Usage() types.Usage {
	if s == nil {
		return types.Usage{}
	}
	if s.result == nil {
		return s.usage
	}
	return s.result.Usage()
}

func (s *StreamRenderSource) FinalStep() types.StepResult {
	if s == nil {
		return types.StepResult{}
	}
	if s.result == nil {
		return s.finalStep
	}
	return s.result.FinalStep()
}
