package websocket

import (
	"context"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"golang.org/x/net/websocket"
)

// Session is the shared state and plumbing every WebSocket-backed provider
// stream (streaming transcription, streaming speech translation) needs,
// parameterized over its stream part type T (provider.TranscriptionStreamPart
// or provider.SpeechTranslationStreamPart). Before this type existed, every
// provider's stream (openai, google, googlevertex, xai, cartesia,
// elevenlabs, gateway) hand-rolled an identical copy of this plumbing —
// the channel of emitted parts, the terminal error, and the connection
// Close() tears down — around its own protocol-specific run()/pumpAudio().
//
// A provider stream type embeds *Session[T] anonymously so Next/Err/Close
// are promoted for free (satisfying provider.TranscriptionStream /
// provider.SpeechTranslationStream, both of which are just
// Next()/Err()/Close()), and drives its own run() goroutine using the
// exported Emit/SetErr/SetConn/CloseParts/CancelContext/Context methods
// below. The protocol-specific parts — message framing, finish logic, finish
// grace timers, clean-close rules, and header/URL construction — stay in
// each provider package; only the plumbing that was byte-for-byte identical
// moved here.
type Session[T any] struct {
	ctx    context.Context
	cancel context.CancelFunc
	parts  chan T

	mu  sync.Mutex
	err error

	closeOnce sync.Once
	connMu    sync.Mutex
	conn      *websocket.Conn
}

// NewSession creates a Session whose context is derived from parentCtx.
// Callers start their own run goroutine and must arrange for CloseParts and
// CancelContext to run (typically both deferred at the top of run) once it
// returns, mirroring every provider's former
// `defer close(s.parts); defer s.cancel()`.
func NewSession[T any](parentCtx context.Context) *Session[T] {
	ctx, cancel := context.WithCancel(parentCtx)
	return &Session[T]{ctx: ctx, cancel: cancel, parts: make(chan T)}
}

// Context returns the session's context, cancelled by Close or CancelContext.
func (s *Session[T]) Context() context.Context { return s.ctx }

// Next implements provider.TranscriptionStream / provider.SpeechTranslationStream.
func (s *Session[T]) Next() (*T, error) {
	part, ok := <-s.parts
	if !ok {
		s.mu.Lock()
		err := s.err
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return &part, nil
}

// Err implements provider.TranscriptionStream / provider.SpeechTranslationStream.
func (s *Session[T]) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close implements provider.TranscriptionStream / provider.SpeechTranslationStream.
func (s *Session[T]) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.connMu.Lock()
		if s.conn != nil {
			_ = s.conn.Close()
		}
		s.connMu.Unlock()
	})
	return nil
}

// SetErr records err as the stream's terminal error, keeping the first one
// set (later calls are no-ops), mirroring every provider's former setErr.
func (s *Session[T]) SetErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// Emit sends part on the session's output channel, returning false when the
// session's context is done (so a blocked send unblocks instead of leaking
// when Close cancels ctx).
func (s *Session[T]) Emit(part T) bool {
	select {
	case s.parts <- part:
		return true
	case <-s.ctx.Done():
		return false
	}
}

// CloseParts closes the output channel; run() goroutines defer this so a
// consumer draining Next() observes EOF/the terminal error once run returns.
func (s *Session[T]) CloseParts() { close(s.parts) }

// CancelContext cancels the session's context directly, releasing its
// resources as soon as run() returns for any reason instead of only on an
// explicit Close() call, which a consumer that only drains Next() to io.EOF
// may never make. Safe to call again later (e.g. from a subsequent Close()):
// cancelling twice is a no-op.
func (s *Session[T]) CancelContext() { s.cancel() }

// SetConn records the active WebSocket connection so Close can close it.
func (s *Session[T]) SetConn(conn *websocket.Conn) {
	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()
}

// ReportError delivers err on errCh, falling back to ctx.Done() so a send
// that no longer has a reader (the receiving run() loop already returned via
// a different path) cannot block the sender forever. Every WS stream's
// pumpAudio uses this to report a failed audio read or socket write,
// mirroring TS's `void sendAudio(socket).catch(finishWithError)`.
func ReportError(ctx context.Context, errCh chan<- error, err error) {
	select {
	case errCh <- err:
	case <-ctx.Done():
	}
}

// PumpAudio drives the send-loop common to every WS stream's sendAudio: read
// chunks from audio until EOF, forwarding each to onChunk; at EOF call onEnd
// once (if non-nil). Any error from audio.Next, onChunk, or onEnd — other
// than one caused by ctx's own cancellation — is reported on errCh via
// ReportError, mirroring TS's `void sendAudio(socket).catch(finishWithError)`
// (a rejected `audioReader.read()` fails the stream exactly like a failed
// `socket.send`). onChunk/onEnd should return a nil error for a condition
// that should merely skip a chunk (e.g. a JSON marshal failure) rather than
// end the pump, mirroring the providers whose sendAudio does `continue`
// rather than fail when a single chunk can't be encoded.
//
// Only providers whose sendAudio fits this exact shape (encode-and-send per
// chunk, one send at EOF) use it; providers with materially different pump
// semantics (turn-detection branching, binary frame splitting, a
// previous_text-on-first-chunk special case) keep their own pumpAudio.
func PumpAudio(ctx context.Context, audio provider.AudioStream, onChunk func([]byte) error, onEnd func() error, errCh chan<- error) {
	for {
		chunk, err := audio.Next(ctx)
		if err != nil {
			if err == io.EOF {
				if onEnd != nil {
					if sendErr := onEnd(); sendErr != nil && ctx.Err() == nil {
						ReportError(ctx, errCh, sendErr)
					}
				}
			} else if ctx.Err() == nil {
				ReportError(ctx, errCh, err)
			}
			return
		}
		if err := onChunk(chunk); err != nil {
			if ctx.Err() == nil {
				ReportError(ctx, errCh, err)
			}
			return
		}
	}
}

// PumpAudioAfterReady is PumpAudio gated on a readiness signal: it blocks
// until ready is closed, or returns immediately (without pumping or
// reporting an error) if ctx is done first. It mirrors the Live API
// streams' sendAudio, which waits for the server's setupComplete
// acknowledgement before sending realtime input.
func PumpAudioAfterReady(ctx context.Context, ready <-chan struct{}, audio provider.AudioStream, onChunk func([]byte) error, onEnd func() error, errCh chan<- error) {
	select {
	case <-ready:
	case <-ctx.Done():
		return
	}
	PumpAudio(ctx, audio, onChunk, onEnd, errCh)
}
