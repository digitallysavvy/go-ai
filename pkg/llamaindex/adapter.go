// Package llamaindex adapts a LlamaIndex chat-engine response stream to AI
// SDK UI message stream chunks.
//
// The TypeScript package (`@ai-sdk/llamaindex`) has no dependency on the real
// `llamaindex` npm package: its `EngineResponse` is a locally-defined
// `{ delta: string }` shape, and the adapter only ever reads `.delta` off of
// it. Go has no LlamaIndex SDK to bind against, so this package defines the
// same minimal shape (EngineResponse) as its input contract. Callers bridge
// whatever LlamaIndex client they use (HTTP, gRPC, a wrapped Python
// subprocess, etc.) into a `<-chan EngineResponse` of delta chunks; this
// package does not talk to LlamaIndex itself.
package llamaindex

import (
	"context"
	"strings"
	"unicode"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// EngineResponse is the minimal Go analog of a LlamaIndex chat-engine
// streamed response chunk, mirroring the TypeScript adapter's local
// `EngineResponse = { delta: string }` type.
type EngineResponse struct {
	Delta string
}

// StreamCallbacks mirrors the TypeScript adapter's stream lifecycle hooks
// (`packages/llamaindex/src/stream-callbacks.ts`). Unlike pkg/langchain's
// richer StreamCallbacks (which also has OnFinish/OnError/OnAbort/
// SendStart/SendFinish for LangGraph's multi-step lifecycle), the LlamaIndex
// adapter only ever produces a single text part, so it exposes exactly the
// four callbacks the TypeScript source does.
type StreamCallbacks struct {
	// OnStart is called once, before any chunk is processed.
	OnStart func() error
	// OnToken is called for every delta chunk, including empty ones
	// produced by leading-whitespace trimming (matches the TypeScript
	// `createCallbacksTransformer`, which calls `onToken` unconditionally
	// for every message it receives).
	OnToken func(token string) error
	// OnText is called for every delta chunk, identically to OnToken. The
	// TypeScript source has a `typeof message === 'string'` guard around
	// its onText call, but delta chunks are always strings in this
	// adapter, so the guard is always true.
	OnText func(text string) error
	// OnFinal is called once, with the full concatenation of every
	// (possibly trimmed) delta chunk, after the input stream ends and
	// before the "text-end" chunk is emitted.
	OnFinal func(completion string) error
}

// ToUIMessageStream converts a LlamaIndex chat-engine response stream into
// AI SDK UI message stream chunks: a single text part (id "1") spanning
// "text-start" / "text-delta"... / "text-end", mirroring
// `packages/llamaindex/src/llamaindex-adapter.ts`'s `toUIMessageStream`.
//
// Leading whitespace is trimmed from the stream exactly once: every delta is
// passed through TrimStart until the first delta that is non-empty *after*
// trimming is seen (mirroring the TypeScript `trimStartOfStream` closure);
// after that, deltas are passed through unmodified, even if empty.
//
// The returned error channel receives at most one error: a callback error
// (from OnStart/OnToken/OnText/OnFinal) or ctx.Err() if ctx is cancelled
// mid-stream. Both channels are closed when the stream is fully drained.
func ToUIMessageStream(ctx context.Context, stream <-chan EngineResponse, callbacks *StreamCallbacks) (<-chan ai.UIMessageChunk, <-chan error) {
	out := make(chan ai.UIMessageChunk)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)

		if callbacks != nil && callbacks.OnStart != nil {
			if err := callbacks.OnStart(); err != nil {
				errs <- err
				return
			}
		}

		if !sendChunk(ctx, out, ai.UIMessageChunk{"type": "text-start", "id": "1"}) {
			errs <- ctx.Err()
			return
		}

		var aggregated []byte
		isStreamStart := true

		for {
			// Check cancellation first: when ctx is done and a delta is
			// also ready, select picks either case at random, so a
			// cancelled stream could still process (and report) one more
			// delta.
			if ctx.Err() != nil {
				errs <- ctx.Err()
				return
			}
			select {
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			case response, ok := <-stream:
				if !ok {
					if callbacks != nil && callbacks.OnFinal != nil {
						if err := callbacks.OnFinal(string(aggregated)); err != nil {
							errs <- err
							return
						}
					}
					if !sendChunk(ctx, out, ai.UIMessageChunk{"type": "text-end", "id": "1"}) {
						errs <- ctx.Err()
						return
					}
					return
				}

				text := response.Delta
				if isStreamStart {
					trimmed := trimStart(text)
					if trimmed != "" {
						isStreamStart = false
					}
					text = trimmed
				}

				aggregated = append(aggregated, text...)

				if callbacks != nil && callbacks.OnToken != nil {
					if err := callbacks.OnToken(text); err != nil {
						errs <- err
						return
					}
				}
				if callbacks != nil && callbacks.OnText != nil {
					if err := callbacks.OnText(text); err != nil {
						errs <- err
						return
					}
				}

				if !sendChunk(ctx, out, ai.UIMessageChunk{"type": "text-delta", "id": "1", "delta": text}) {
					errs <- ctx.Err()
					return
				}
			}
		}
	}()

	return out, errs
}

// trimStart mirrors JavaScript's String.prototype.trimStart: it strips
// leading Unicode whitespace only, matching `text.trimStart()` in the
// TypeScript adapter.
func trimStart(text string) string {
	return strings.TrimLeftFunc(text, unicode.IsSpace)
}

func sendChunk(ctx context.Context, out chan<- ai.UIMessageChunk, chunk ai.UIMessageChunk) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- chunk:
		return true
	}
}
