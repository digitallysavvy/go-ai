package ai

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// ChunkDetector finds the first complete chunk at the start of buffer.
// It returns the matched prefix and true when a chunk was found, or
// ("", false) when the buffer does not yet contain a complete chunk.
//
// Mirrors the TypeScript SDK's smoothStream ChunkDetector type
// (`(buffer: string) => string | undefined | null`).
type ChunkDetector func(buffer string) (string, bool)

// smoothStreamWhitespaceClass approximates JavaScript's `\s` character
// class, which is wider than RE2's `\s` (ASCII-only): it additionally
// matches NBSP, several Unicode space separators, and U+FEFF (BOM), which
// ECMAScript treats as whitespace for historical reasons. Written with
// RE2's `\x{hex}` escapes (plain ASCII in the Go source) rather than Go
// string Unicode escapes, to avoid embedding literal multi-byte runes
// (in particular U+FEFF itself, which would otherwise appear as a BOM in
// the middle of this source file) in the .go file.
const smoothStreamWhitespaceClass = `\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

var (
	smoothStreamWordRegexp = regexp.MustCompile(`[^` + smoothStreamWhitespaceClass + `]+[` + smoothStreamWhitespaceClass + `]+`)
	smoothStreamLineRegexp = regexp.MustCompile(`\n+`)
)

// SmoothStreamOptions configures SmoothStream.
//
// Mirrors the TypeScript SDK's smoothStream options (generate-text/smooth-stream.ts),
// minus the browser-only Intl.Segmenter and hidden-document handling, which
// have no Go equivalent.
type SmoothStreamOptions struct {
	// DelayInMs is the delay between emitted chunks. nil means unset and
	// defaults to 10ms (TS default); a non-nil pointer to 0 disables the
	// delay entirely (TS `delayInMs: null`). This mirrors the *int
	// "unset vs explicit zero" convention used elsewhere in this package
	// (see GenerateTextOptions.MaxRetries), applied to TS's `number | null`.
	DelayInMs *int

	// Chunking controls how text is split for streaming. One of:
	//   - "word" (default): split on `\S+\s+` (word boundaries).
	//   - "line": split on `\n+`.
	//   - a *regexp.Regexp: must not match an empty string.
	//   - a ChunkDetector function.
	Chunking interface{}

	// internalDelay overrides the delay function. Test-only.
	internalDelay func(ctx context.Context, delayInMs *int)
}

// smoothStreamDetector is the internal, error-aware detector used once
// Chunking has been resolved and validated. A non-nil error means the
// buffer contains an invalid match (empty regex match, or a caller-supplied
// ChunkDetector that violated its contract) and streaming must stop.
type smoothStreamDetector func(buffer string) (match string, err error)

func regexSmoothStreamDetector(re *regexp.Regexp) smoothStreamDetector {
	return func(buffer string) (string, error) {
		loc := re.FindStringIndex(buffer)
		if loc == nil {
			return "", nil
		}
		if loc[1] == loc[0] {
			return "", fmt.Errorf("Chunking RegExp must not match an empty string.")
		}
		return buffer[:loc[1]], nil
	}
}

func customSmoothStreamDetector(fn ChunkDetector) smoothStreamDetector {
	return func(buffer string) (string, error) {
		match, ok := fn(buffer)
		if !ok {
			return "", nil
		}
		if match == "" {
			return "", fmt.Errorf("Chunking function must return a non-empty string.")
		}
		if !strings.HasPrefix(buffer, match) {
			return "", fmt.Errorf("Chunking function must return a match that is a prefix of the buffer. Received: %q expected to start with %q", match, buffer)
		}
		return match, nil
	}
}

func resolveSmoothStreamDetector(chunking interface{}) (smoothStreamDetector, error) {
	switch c := chunking.(type) {
	case nil:
		return regexSmoothStreamDetector(smoothStreamWordRegexp), nil
	case string:
		switch c {
		case "word":
			return regexSmoothStreamDetector(smoothStreamWordRegexp), nil
		case "line":
			return regexSmoothStreamDetector(smoothStreamLineRegexp), nil
		default:
			return nil, newSmoothStreamChunkingError(chunking)
		}
	case *regexp.Regexp:
		return regexSmoothStreamDetector(c), nil
	case ChunkDetector:
		return customSmoothStreamDetector(c), nil
	case func(string) (string, bool):
		return customSmoothStreamDetector(c), nil
	default:
		return nil, newSmoothStreamChunkingError(chunking)
	}
}

// newSmoothStreamChunkingError mirrors TS smoothStream's
// `throw new InvalidArgumentError({argument: 'chunking', message: ...})`.
func newSmoothStreamChunkingError(chunking interface{}) error {
	return &providererrors.InvalidArgumentError{
		Field:   "chunking",
		Message: fmt.Sprintf("chunking must be \"word\", \"line\", a *regexp.Regexp, or a ChunkDetector function. Received: %v", chunking),
	}
}

// smoothStreamSleep sleeps for delayInMs, honoring ctx cancellation. A nil
// delayInMs skips the delay entirely (TS `delayInMs: null`).
func smoothStreamSleep(ctx context.Context, delayInMs *int) {
	if delayInMs == nil || *delayInMs <= 0 {
		return
	}
	timer := time.NewTimer(time.Duration(*delayInMs) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// SmoothStream returns a StreamTransformFunc that smooths text and
// reasoning streaming output by buffering deltas and re-emitting them in
// smaller, evenly-paced chunks (word by word, line by line, or per a custom
// pattern/detector). Create one instance per StreamText call: the returned
// function is stateful (it holds a buffer across calls).
//
// Mirrors the TypeScript SDK's smoothStream (generate-text/smooth-stream.ts):
// the TS transform emits each detected chunk to the consumer immediately and
// then awaits the delay before looking for the next one, so downstream
// readers see chunks trickle in over time rather than arriving all at once.
//
// When called by StreamText (which installs an emitter via
// WithStreamTransformEmitter around every ExperimentalTransform invocation),
// this function emits each match through that emitter immediately and sleeps
// between them, matching TS's per-chunk pacing. Called directly without an
// installed emitter (e.g. in a test, or from any other caller), it falls
// back to the batch behavior: every chunk produced from a single input delta
// is still paced internally with the same delay and call counts, but is
// returned together as a slice once the whole delta has been processed.
func SmoothStream(opts SmoothStreamOptions) (StreamTransformFunc, error) {
	detect, err := resolveSmoothStreamDetector(opts.Chunking)
	if err != nil {
		return nil, err
	}

	delayInMs := opts.DelayInMs
	if delayInMs == nil {
		ten := 10
		delayInMs = &ten
	}

	sleep := opts.internalDelay
	if sleep == nil {
		sleep = smoothStreamSleep
	}

	var (
		buffer      strings.Builder
		id          string
		chunkType   provider.ChunkType
		metadata    []byte
		streamEnded bool
	)

	// makeDeltaChunk builds an output text-delta/reasoning-delta chunk,
	// writing the payload into the field the chunk type carries content in
	// (Text for ChunkTypeText, Reasoning for ChunkTypeReasoning).
	makeDeltaChunk := func(t provider.ChunkType, chunkID, text string) provider.StreamChunk {
		out := provider.StreamChunk{Type: t, ID: chunkID}
		if t == provider.ChunkTypeReasoning {
			out.Reasoning = text
		} else {
			out.Text = text
		}
		return out
	}

	flush := func() []provider.StreamChunk {
		if chunkType == "" || (buffer.Len() == 0 && metadata == nil) {
			return nil
		}
		out := makeDeltaChunk(chunkType, id, buffer.String())
		if metadata != nil {
			out.ProviderMetadata = metadata
		}
		buffer.Reset()
		metadata = nil
		return []provider.StreamChunk{out}
	}

	return func(ctx context.Context, chunk provider.StreamChunk) []provider.StreamChunk {
		// When called from StreamText's transform pipeline, an emitter is
		// installed in ctx: send forwards each chunk to it immediately
		// (mirroring TS, which enqueues each match and awaits the delay
		// before the next one). Without an emitter, send falls back to
		// accumulating into the returned batch, preserving the previous
		// behavior for direct callers (e.g. tests).
		emit, hasEmitter := StreamTransformEmitterFromContext(ctx)
		var out []provider.StreamChunk
		send := func(c provider.StreamChunk) {
			if hasEmitter {
				emit(c)
				return
			}
			out = append(out, c)
		}

		if streamEnded {
			return []provider.StreamChunk{chunk}
		}

		// Handle non-smoothable chunks: flush the buffer and pass through.
		if chunk.Type != provider.ChunkTypeText && chunk.Type != provider.ChunkTypeReasoning {
			for _, c := range flush() {
				send(c)
			}
			return append(out, chunk)
		}

		text := chunk.Text
		if chunk.Type == provider.ChunkTypeReasoning && chunk.Reasoning != "" {
			text = chunk.Reasoning
		}

		// Flush the buffer when the block type or ID changes; never carry
		// metadata from one part to another.
		if (chunk.Type != chunkType || chunk.ID != id) && (buffer.Len() > 0 || metadata != nil) {
			for _, c := range flush() {
				send(c)
			}
		}

		buffer.WriteString(text)
		id = chunk.ID
		chunkType = chunk.Type

		if chunk.ProviderMetadata != nil {
			metadata = chunk.ProviderMetadata
		}

		for {
			match, derr := detect(buffer.String())
			if derr != nil {
				streamEnded = true
				out = append(out, provider.StreamChunk{Type: provider.ChunkTypeError, Text: derr.Error()})
				buffer.Reset()
				metadata = nil
				return out
			}
			if match == "" {
				break
			}
			send(makeDeltaChunk(chunkType, id, match))
			remaining := buffer.String()[len(match):]
			buffer.Reset()
			buffer.WriteString(remaining)

			sleep(ctx, delayInMs)
		}

		return out
	}, nil
}
