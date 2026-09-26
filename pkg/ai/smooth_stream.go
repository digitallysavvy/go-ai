package ai

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
			return nil, fmt.Errorf("chunking must be \"word\", \"line\", a *regexp.Regexp, or a ChunkDetector function. Received: %v", chunking)
		}
	case *regexp.Regexp:
		return regexSmoothStreamDetector(c), nil
	case ChunkDetector:
		return customSmoothStreamDetector(c), nil
	case func(string) (string, bool):
		return customSmoothStreamDetector(c), nil
	default:
		return nil, fmt.Errorf("chunking must be \"word\", \"line\", a *regexp.Regexp, or a ChunkDetector function. Received: %v", chunking)
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
// Mirrors the TypeScript SDK's smoothStream (generate-text/smooth-stream.ts).
//
// # Delay hook (does not yet exist)
//
// The TS transform emits each detected chunk to the consumer immediately
// and then awaits the delay before looking for the next one, so downstream
// readers see chunks trickle in over time. Go's StreamTransformFunc has the
// shape `func(ctx, chunk) []provider.StreamChunk`: it can only return a
// batch, which stream.go forwards to the consumer only after the function
// returns. As implemented here, the delay still happens (paced between
// each detected match) but it delays the return of the whole batch rather
// than the delivery of each individual chunk — every chunk produced from a
// single input delta reaches the consumer at the same instant, once all of
// that delta's internal delays have elapsed. This preserves total pacing
// and delay call counts but not per-chunk incremental delivery.
//
// True incremental delivery requires stream.go to expose an emitter hook,
// e.g.:
//
//	type StreamTransformEmitter func(provider.StreamChunk)
//	func WithStreamTransformEmitter(ctx context.Context, emit StreamTransformEmitter) context.Context
//	func StreamTransformEmitterFromContext(ctx context.Context) (StreamTransformEmitter, bool)
//
// wired into the ExperimentalTransform loop (around stream.go:1138-1146) so
// that when a transform calls the emitter, stream.go forwards that chunk
// (onChunk + FireOnChunk) immediately instead of waiting for the transform
// call to return, and treats the transform's returned slice as "any
// remaining chunks not already emitted". See the implementation PR report
// for the exact proposed diff.
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
		if streamEnded {
			return []provider.StreamChunk{chunk}
		}

		// Handle non-smoothable chunks: flush the buffer and pass through.
		if chunk.Type != provider.ChunkTypeText && chunk.Type != provider.ChunkTypeReasoning {
			out := flush()
			return append(out, chunk)
		}

		text := chunk.Text
		if chunk.Type == provider.ChunkTypeReasoning && chunk.Reasoning != "" {
			text = chunk.Reasoning
		}

		var out []provider.StreamChunk

		// Flush the buffer when the block type or ID changes; never carry
		// metadata from one part to another.
		if (chunk.Type != chunkType || chunk.ID != id) && (buffer.Len() > 0 || metadata != nil) {
			out = append(out, flush()...)
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
			out = append(out, makeDeltaChunk(chunkType, id, match))
			remaining := buffer.String()[len(match):]
			buffer.Reset()
			buffer.WriteString(remaining)

			sleep(ctx, delayInMs)
		}

		return out
	}, nil
}
