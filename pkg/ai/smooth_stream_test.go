package ai

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// runSmoothStream feeds chunks through fn sequentially (mirroring TS
// convertArrayToReadableStream(...).pipeThrough(smoothStream()...)) and
// returns the concatenated output, plus the number of times the delay hook
// was invoked.
func runSmoothStream(fn StreamTransformFunc, chunks []provider.StreamChunk) (out []provider.StreamChunk) {
	ctx := context.Background()
	for _, c := range chunks {
		out = append(out, fn(ctx, c)...)
	}
	return out
}

func textChunk(id, text string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeText, ID: id, Text: text}
}

func reasoningChunk(id, text string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: id, Reasoning: text}
}

func textTexts(chunks []provider.StreamChunk) []string {
	var out []string
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeText {
			out = append(out, c.Text)
		}
	}
	return out
}

func strSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TS smooth-stream.test.ts "word chunking > should combine partial words"
func TestSmoothStream_WordChunking_CombinePartialWords(t *testing.T) {
	t.Parallel()

	var delays int
	fn, err := SmoothStream(SmoothStreamOptions{
		internalDelay: func(context.Context, *int) { delays++ },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", "Hello"),
		textChunk("1", ", "),
		textChunk("1", "world!"),
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	})

	got := textTexts(out)
	want := []string{"Hello, ", "world!"}
	if !strSlicesEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
	if delays != 1 {
		t.Errorf("delays = %d, want 1", delays)
	}
	if out[len(out)-1].Type != provider.ChunkTypeTextEnd {
		t.Errorf("expected text-end to pass through unchanged, got %+v", out[len(out)-1])
	}
}

// TS smooth-stream.test.ts "word chunking > should split larger text chunks"
func TestSmoothStream_WordChunking_SplitLargerChunks(t *testing.T) {
	t.Parallel()

	var delays int
	fn, err := SmoothStream(SmoothStreamOptions{
		internalDelay: func(context.Context, *int) { delays++ },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", "Hello, World! This is an example text."),
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	})

	got := textTexts(out)
	want := []string{"Hello, ", "World! ", "This ", "is ", "an ", "example ", "text."}
	if !strSlicesEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
	if delays != len(want)-1 {
		t.Errorf("delays = %d, want %d (one per emitted chunk except the trailing partial)", delays, len(want)-1)
	}
}

// TS smooth-stream.test.ts "word chunking > doesn't return chunks with just spaces"
func TestSmoothStream_WordChunking_NoSpaceOnlyChunks(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", " "),
		textChunk("1", " "),
		textChunk("1", " "),
		textChunk("1", "foo"),
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	})

	got := textTexts(out)
	want := []string{"   foo"}
	if !strSlicesEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
}

// TS smooth-stream.test.ts "word chunking > should send remaining text buffer before tool call starts"
func TestSmoothStream_FlushesBeforeNonTextChunk(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	toolCall := provider.StreamChunk{Type: provider.ChunkTypeToolCall}
	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", "I will check the"),
		textChunk("1", " weather in Lon"),
		textChunk("1", "don."),
		toolCall,
	})

	got := textTexts(out)
	want := []string{"I ", "will ", "check ", "the ", "weather ", "in ", "London."}
	if !strSlicesEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
	if out[len(out)-1].Type != provider.ChunkTypeToolCall {
		t.Fatalf("expected the tool-call chunk to pass through last, got %+v", out[len(out)-1])
	}
}

// TS smooth-stream.test.ts "line chunking > should split text by lines"
func TestSmoothStream_LineChunking(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{
		Chunking:      "line",
		internalDelay: func(context.Context, *int) {},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", "First line\nSecond line\nThird line with more text\n"),
		textChunk("1", "Partial line"),
		textChunk("1", " continues\nFinal line\n"),
	})

	got := textTexts(out)
	want := []string{
		"First line\n",
		"Second line\n",
		"Third line with more text\n",
		"Partial line continues\n",
		"Final line\n",
	}
	if !strSlicesEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
}

// TS smooth-stream.test.ts "line chunking > should handle text without line endings"
func TestSmoothStream_LineChunking_NoLineEndings(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{Chunking: "line"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", "Text without"),
		textChunk("1", " any line"),
		textChunk("1", " breaks"),
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	})

	got := textTexts(out)
	want := []string{"Text without any line breaks"}
	if !strSlicesEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
}

// TS smooth-stream.test.ts "custom chunking > should return correct result
// for regexes that don't match from the exact start onwards"
func TestSmoothStream_CustomRegexNotAnchoredAtStart(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{Chunking: regexp.MustCompile(`_`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", "Hello_, world!"),
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	})

	got := textTexts(out)
	want := []string{"Hello_", ", world!"}
	if !strSlicesEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
}

// TS smooth-stream.test.ts "custom chunking > should support custom chunking
// regexps (character-level)"
func TestSmoothStream_CustomRegexCharacterLevel(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{Chunking: regexp.MustCompile(`.`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", "Hi!"),
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	})

	got := textTexts(out)
	want := []string{"H", "i", "!"}
	if !strSlicesEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
}

// TS smooth-stream.test.ts "custom chunking > throws when a regexp produces
// an empty match": the regexp /\S*/ matches the whole word, then matches an
// empty string against the now-empty buffer, which must surface as an error
// chunk (the transform API cannot return a Go error).
func TestSmoothStream_RegexEmptyMatchIsError(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{Chunking: regexp.MustCompile(`\S*`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", "hello"),
	})

	var found bool
	for _, c := range out {
		if c.Type == provider.ChunkTypeError {
			found = true
			if !strings.Contains(c.Text, "Chunking RegExp must not match an empty string.") {
				t.Errorf("error text = %q", c.Text)
			}
		}
	}
	if !found {
		t.Fatalf("expected a ChunkTypeError chunk, got %+v", out)
	}
}

// TS smooth-stream.test.ts "custom callback chunking > should support custom
// chunking callback"
func TestSmoothStream_CustomChunkDetector(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(`[^_]*_`)
	detector := ChunkDetector(func(buffer string) (string, bool) {
		loc := re.FindStringIndex(buffer)
		if loc == nil {
			return "", false
		}
		return buffer[:loc[1]], true
	})

	fn, err := SmoothStream(SmoothStreamOptions{Chunking: detector})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", "He_llo, "),
		textChunk("1", "w_orld!"),
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	})

	got := textTexts(out)
	want := []string{"He_", "llo, w_", "orld!"}
	if !strSlicesEqual(got, want) {
		t.Fatalf("texts = %#v, want %#v", got, want)
	}
}

// TS smooth-stream.test.ts "custom callback chunking > throws empty match error"
func TestSmoothStream_ChunkDetectorEmptyMatchIsError(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{
		Chunking: ChunkDetector(func(string) (string, bool) { return "", true }),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{textChunk("1", "Hello, world!")})

	if len(out) == 0 || out[0].Type != provider.ChunkTypeError {
		t.Fatalf("expected a ChunkTypeError chunk, got %+v", out)
	}
	if !strings.Contains(out[0].Text, "Chunking function must return a non-empty string.") {
		t.Errorf("error text = %q", out[0].Text)
	}
}

// TS smooth-stream.test.ts "custom callback chunking > throws match prefix error"
func TestSmoothStream_ChunkDetectorNonPrefixMatchIsError(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{
		Chunking: ChunkDetector(func(string) (string, bool) { return "world", true }),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{textChunk("1", "Hello, world!")})

	if len(out) == 0 || out[0].Type != provider.ChunkTypeError {
		t.Fatalf("expected a ChunkTypeError chunk, got %+v", out)
	}
	if !strings.Contains(out[0].Text, "must be a prefix") && !strings.Contains(out[0].Text, "expected to start with") {
		t.Errorf("error text = %q", out[0].Text)
	}
}

// TS smooth-stream.test.ts "throws error if chunking option is invalid" /
// "... is null" (Go: an unsupported chunking value is a constructor error).
func TestSmoothStream_InvalidChunkingOption(t *testing.T) {
	t.Parallel()

	if _, err := SmoothStream(SmoothStreamOptions{Chunking: "foo"}); err == nil {
		t.Error("expected an error for an invalid chunking string")
	}
	if _, err := SmoothStream(SmoothStreamOptions{Chunking: 42}); err == nil {
		t.Error("expected an error for an unsupported chunking type")
	}
}

// TS smooth-stream.test.ts "delay > should default to 10ms" /
// "should support different number of milliseconds delay" / "should support
// null delay".
func TestSmoothStream_DelayConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("default", func(t *testing.T) {
		t.Parallel()
		var got *int
		fn, err := SmoothStream(SmoothStreamOptions{
			internalDelay: func(_ context.Context, ms *int) { got = ms },
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		runSmoothStream(fn, []provider.StreamChunk{textChunk("1", "Hello, world!")})
		if got == nil || *got != 10 {
			t.Fatalf("delay = %v, want 10", got)
		}
	})

	t.Run("custom", func(t *testing.T) {
		t.Parallel()
		twenty := 20
		var got *int
		fn, err := SmoothStream(SmoothStreamOptions{
			DelayInMs:     &twenty,
			internalDelay: func(_ context.Context, ms *int) { got = ms },
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		runSmoothStream(fn, []provider.StreamChunk{textChunk("1", "Hello, world!")})
		if got == nil || *got != 20 {
			t.Fatalf("delay = %v, want 20", got)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		zero := 0
		var calls int
		fn, err := SmoothStream(SmoothStreamOptions{
			DelayInMs:     &zero,
			internalDelay: func(context.Context, *int) { calls++ },
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := runSmoothStream(fn, []provider.StreamChunk{textChunk("1", "Hello, world!")})
		if calls != 1 {
			t.Fatalf("delay hook calls = %d, want 1 (still invoked, just a no-op default sleep)", calls)
		}
		if len(textTexts(out)) == 0 {
			t.Fatal("expected at least one emitted chunk")
		}
	})
}

// TS smooth-stream.test.ts "text part id changes > should change the id when
// the text part id changes"
func TestSmoothStream_FlushesOnIDChange(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		textChunk("1", "partial"),
		textChunk("2", "next part "),
		{Type: provider.ChunkTypeTextEnd, ID: "2"},
	})

	if len(out) < 2 {
		t.Fatalf("expected at least 2 chunks, got %+v", out)
	}
	if out[0].Text != "partial" || out[0].ID != "1" {
		t.Errorf("first flushed chunk = %+v, want text=partial id=1", out[0])
	}
	for _, c := range out[1:] {
		if c.Type == provider.ChunkTypeText && c.ID != "2" {
			t.Errorf("expected subsequent text chunks to carry id=2, got %+v", c)
		}
	}
}

// TS smooth-stream.test.ts "reasoning smoothing > should combine partial
// reasoning words"
func TestSmoothStream_ReasoningChunking(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := runSmoothStream(fn, []provider.StreamChunk{
		reasoningChunk("r1", "Let me "),
		reasoningChunk("r1", "think"),
		{Type: provider.ChunkTypeReasoningEnd, ID: "r1"},
	})

	var got []string
	for _, c := range out {
		if c.Type == provider.ChunkTypeReasoning {
			got = append(got, c.Reasoning)
		}
	}
	// "Let me " already contains two complete words ("Let " and "me "), so
	// both are emitted as soon as they arrive; "think" has no trailing
	// whitespace and is only flushed at reasoning-end.
	want := []string{"Let ", "me ", "think"}
	if !strSlicesEqual(got, want) {
		t.Fatalf("reasoning texts = %#v, want %#v", got, want)
	}
	if out[len(out)-1].Type != provider.ChunkTypeReasoningEnd {
		t.Errorf("expected reasoning-end to pass through unchanged, got %+v", out[len(out)-1])
	}
}

// TS smooth-stream.test.ts "providerMetadata preservation > should preserve
// metadata from an empty delta when the buffer is empty": an empty
// reasoning-delta carrying providerMetadata must still be flushed (with an
// empty text) once a boundary is hit, instead of being silently dropped.
func TestSmoothStream_PreservesMetadataFromEmptyDelta(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta := []byte(`{"anthropic":{"signature":"sig_abc123"}}`)
	out := runSmoothStream(fn, []provider.StreamChunk{
		reasoningChunk("r1", "Let me think. "),
		{Type: provider.ChunkTypeReasoning, ID: "r1", Reasoning: "", ProviderMetadata: meta},
		{Type: provider.ChunkTypeReasoningEnd, ID: "r1"},
	})

	var last *provider.StreamChunk
	for i := range out {
		if out[i].Type == provider.ChunkTypeReasoning {
			last = &out[i]
		}
	}
	if last == nil {
		t.Fatal("expected a trailing reasoning chunk carrying the metadata")
	}
	if last.Text != "" && last.Reasoning != "" {
		t.Errorf("expected the metadata-carrying flush to have empty text, got %+v", last)
	}
	if string(last.ProviderMetadata) != string(meta) {
		t.Errorf("ProviderMetadata = %s, want %s", last.ProviderMetadata, meta)
	}
}

// TS smooth-stream.test.ts "providerMetadata preservation > should preserve
// providerMetadata on reasoning-start for redacted thinking": non-delta
// boundary chunks pass through with their own metadata untouched.
func TestSmoothStream_PassesThroughBoundaryChunkMetadata(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta := []byte(`{"anthropic":{"redactedData":"redacted-thinking-data"}}`)
	out := runSmoothStream(fn, []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoningStart, ID: "1", ProviderMetadata: meta},
		{Type: provider.ChunkTypeReasoningEnd, ID: "1"},
	})

	if len(out) != 2 {
		t.Fatalf("expected 2 passthrough chunks, got %+v", out)
	}
	if string(out[0].ProviderMetadata) != string(meta) {
		t.Errorf("reasoning-start ProviderMetadata = %s, want %s", out[0].ProviderMetadata, meta)
	}
}

// deltaText returns the payload text of a text-delta/reasoning-delta chunk
// (Text for text, Reasoning for reasoning), mirroring TextStreamPart's
// unified `.text` field in TS.
func deltaText(c provider.StreamChunk) string {
	if c.Type == provider.ChunkTypeReasoning {
		return c.Reasoning
	}
	return c.Text
}

// assertSmoothStreamChunks fails the test unless got and want have the same
// length, type, id, text/reasoning payload, and ProviderMetadata bytes at
// every position, in order.
func assertSmoothStreamChunks(t *testing.T, got []provider.StreamChunk, want []provider.StreamChunk) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d chunks, want %d: got=%+v want=%+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Type != w.Type || g.ID != w.ID || deltaText(g) != deltaText(w) || string(g.ProviderMetadata) != string(w.ProviderMetadata) {
			t.Errorf("chunk %d = %+v, want %+v", i, g, w)
		}
	}
}

// TS smooth-stream.test.ts "providerMetadata preservation > should preserve
// providerMetadata on every $chunking-chunked $deltaType part" (TS #19561):
// every chunk split out of a single metadata-bearing delta, including the
// final one emitted via the trailing flush, must carry that metadata, not
// just the first (previously dropped from loop-emitted chunks) or last.
func TestSmoothStream_PreservesMetadataOnEveryChunkedPart(t *testing.T) {
	t.Parallel()

	meta := []byte(`{"anthropic":{"signature":"sig_abc123"}}`)

	tests := []struct {
		name         string
		chunking     string
		delta        provider.StreamChunk
		end          provider.StreamChunk
		expectedText []string
	}{
		{
			name:         "word-chunked reasoning-delta",
			chunking:     "word",
			delta:        provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "First second final", ProviderMetadata: meta},
			end:          provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "1"},
			expectedText: []string{"First ", "second ", "final"},
		},
		{
			name:         "line-chunked text-delta",
			chunking:     "line",
			delta:        provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "First line\nSecond line\nfinal line", ProviderMetadata: meta},
			end:          provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
			expectedText: []string{"First line\n", "Second line\n", "final line"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fn, err := SmoothStream(SmoothStreamOptions{Chunking: tt.chunking, internalDelay: func(context.Context, *int) {}})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			out := runSmoothStream(fn, []provider.StreamChunk{tt.delta, tt.end})

			var deltas []provider.StreamChunk
			for _, c := range out {
				if c.Type == tt.delta.Type {
					deltas = append(deltas, c)
				}
			}
			want := make([]provider.StreamChunk, len(tt.expectedText))
			for i, text := range tt.expectedText {
				want[i] = provider.StreamChunk{Type: tt.delta.Type, ID: "1", ProviderMetadata: meta}
				if tt.delta.Type == provider.ChunkTypeReasoning {
					want[i].Reasoning = text
				} else {
					want[i].Text = text
				}
			}
			assertSmoothStreamChunks(t, deltas, want)
		})
	}
}

// TS smooth-stream.test.ts "providerMetadata preservation > should preserve
// an empty metadata delta after an exact boundary" (TS #19561).
func TestSmoothStream_PreservesEmptyMetadataDeltaAfterExactBoundary(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metaA := []byte(`{"anthropic":{"signature":"sig-a"}}`)
	out := runSmoothStream(fn, []provider.StreamChunk{
		reasoningChunk("1", "Done "),
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "", ProviderMetadata: metaA},
		{Type: provider.ChunkTypeReasoningEnd, ID: "1"},
	})

	assertSmoothStreamChunks(t, out, []provider.StreamChunk{
		reasoningChunk("1", "Done "),
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "", ProviderMetadata: metaA},
		{Type: provider.ChunkTypeReasoningEnd, ID: "1"},
	})
}

// TS smooth-stream.test.ts "providerMetadata preservation > should not
// carry metadata to a metadata-free delta with the same id" (TS #19561): a
// pending metadata-bearing buffer must be flushed (carrying its metadata)
// before a later metadata-free delta for the same id/type is merged in, so
// the metadata never attaches to text it didn't originate from.
func TestSmoothStream_DoesNotCarryMetadataToMetadataFreeDeltaSameID(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metaA := []byte(`{"anthropic":{"signature":"sig-a"}}`)
	out := runSmoothStream(fn, []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "Signed", ProviderMetadata: metaA},
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: " plain "},
	})

	assertSmoothStreamChunks(t, out, []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "Signed", ProviderMetadata: metaA},
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: " plain "},
	})
}

// TS smooth-stream.test.ts "providerMetadata preservation > should keep
// different metadata values with their source deltas" (TS #19561).
func TestSmoothStream_KeepsDifferentMetadataValuesWithSourceDeltas(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metaA := []byte(`{"anthropic":{"signature":"sig-a"}}`)
	metaB := []byte(`{"anthropic":{"signature":"sig-b"}}`)
	out := runSmoothStream(fn, []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "First", ProviderMetadata: metaA},
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: " second ", ProviderMetadata: metaB},
	})

	assertSmoothStreamChunks(t, out, []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "First", ProviderMetadata: metaA},
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: " second ", ProviderMetadata: metaB},
	})
}

// TS smooth-stream.test.ts "providerMetadata preservation > should not
// carry word-chunk metadata to a subsequent delta type" (TS #19561).
func TestSmoothStream_DoesNotCarryWordChunkMetadataToSubsequentDeltaType(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta := []byte(`{"anthropic":{"signature":"sig_abc123"}}`)
	out := runSmoothStream(fn, []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoning, ID: "reasoning-1", Reasoning: "Signed ", ProviderMetadata: meta},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "Plain "},
	})

	assertSmoothStreamChunks(t, out, []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoning, ID: "reasoning-1", Reasoning: "Signed ", ProviderMetadata: meta},
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "Plain "},
	})
}

// TS smooth-stream.test.ts "providerMetadata preservation > should not
// carry line-chunk metadata to a subsequent delta id" (TS #19561).
func TestSmoothStream_DoesNotCarryLineChunkMetadataToSubsequentDeltaID(t *testing.T) {
	t.Parallel()

	fn, err := SmoothStream(SmoothStreamOptions{Chunking: "line", internalDelay: func(context.Context, *int) {}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	meta := []byte(`{"anthropic":{"signature":"sig_abc123"}}`)
	out := runSmoothStream(fn, []provider.StreamChunk{
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "Signed line\n", ProviderMetadata: meta},
		{Type: provider.ChunkTypeText, ID: "text-2", Text: "Plain line\n"},
	})

	assertSmoothStreamChunks(t, out, []provider.StreamChunk{
		{Type: provider.ChunkTypeText, ID: "text-1", Text: "Signed line\n", ProviderMetadata: meta},
		{Type: provider.ChunkTypeText, ID: "text-2", Text: "Plain line\n"},
	})
}
