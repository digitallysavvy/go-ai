package middleware

import (
	"context"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ExtractJSONOptions configures the JSON extraction middleware
type ExtractJSONOptions struct {
	// Transform is a custom function to extract JSON from text.
	// If nil, the default transform strips markdown code fences.
	Transform func(text string) string
}

// defaultJSONTransform strips markdown code fences from text
func defaultJSONTransform(text string) string {
	// Remove opening fence: ```json or ```
	text = jsonFencePrefixRe.ReplaceAllString(text, "")
	// Remove closing fence: ```
	text = jsonFenceSuffixRe.ReplaceAllString(text, "")
	return providerutils.JSTrim(text)
}

// jsonFenceSuffixRe matches a trailing markdown code fence, mirroring TS
// stripMarkdownCodeFenceSuffix's regex.
var jsonFenceSuffixRe = regexp.MustCompile(`\n?` + "```" + jsWS + `*$`)

// jsWS is the character class of JavaScript's regex \s (RE2's \s is ASCII
// only and omits \v, NBSP, U+FEFF and the Unicode spaces).
const jsWS = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var jsonFencePrefixRe = regexp.MustCompile(`^` + "```" + `(?:json)?` + jsWS + `*\n?`)

// stripMarkdownCodeFenceSuffix removes a trailing markdown code fence (with
// an optional leading newline) and trims trailing whitespace only — it must
// never trim leading whitespace, since earlier text may already have
// streamed out.
func stripMarkdownCodeFenceSuffix(text string) string {
	return providerutils.JSTrimEnd(jsonFenceSuffixRe.ReplaceAllString(text, ""))
}

// getPotentialSuffixStart returns the byte offset from which text could
// still turn into trailing whitespace or a markdown code fence suffix (up to
// three backticks, plus any whitespace on either side). Streaming callers
// hold back everything from this offset onward so a fence split across
// chunks is never leaked into already-emitted output. Mirrors TS
// getPotentialSuffixStart (audit row 1058ed5 / WG12).
func getPotentialSuffixStart(text string) int {
	runes := []rune(text)
	index := len(runes)

	for index > 0 && providerutils.IsJSWhiteSpace(runes[index-1]) {
		index--
	}

	backtickCount := 0
	for index > 0 && backtickCount < 3 && runes[index-1] == '`' {
		index--
		backtickCount++
	}

	if backtickCount > 0 {
		for index > 0 && providerutils.IsJSWhiteSpace(runes[index-1]) {
			index--
		}
	}

	return len(string(runes[:index]))
}

// ExtractJSONMiddleware returns middleware that extracts JSON from text content
// by stripping markdown code fences and other formatting.
//
// This is useful when using structured output with models that wrap
// JSON responses in markdown code blocks.
//
// Example:
//
//	middleware := ExtractJSONMiddleware(nil) // Use default transform
//	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)
//
//	// Or with custom transform:
//	middleware := ExtractJSONMiddleware(&ExtractJSONOptions{
//		Transform: func(text string) string {
//			// Custom extraction logic
//			return strings.TrimPrefix(text, "JSON: ")
//		},
//	})
func ExtractJSONMiddleware(options *ExtractJSONOptions) *LanguageModelMiddleware {
	transform := defaultJSONTransform
	hasCustomTransform := false

	if options != nil && options.Transform != nil {
		transform = options.Transform
		hasCustomTransform = true
	}

	return &LanguageModelMiddleware{
		SpecificationVersion: "v3",

		WrapGenerate: func(
			ctx context.Context,
			doGenerate func() (*types.GenerateResult, error),
			doStream func() (provider.TextStream, error),
			params *provider.GenerateOptions,
			model provider.LanguageModel,
		) (*types.GenerateResult, error) {
			result, err := doGenerate()
			if err != nil {
				return nil, err
			}

			// Transform every text part (TS maps over content).
			mapGenerateTextParts(result, func(part types.TextContent) []types.ContentPart {
				part.Text = transform(part.Text)
				return []types.ContentPart{part}
			})
			return result, nil
		},

		WrapStream: func(
			ctx context.Context,
			doGenerate func() (*types.GenerateResult, error),
			doStream func() (provider.TextStream, error),
			params *provider.GenerateOptions,
			model provider.LanguageModel,
		) (provider.TextStream, error) {
			stream, err := doStream()
			if err != nil {
				return nil, err
			}

			return newStreamingExtractJSONWrapper(stream, transform, hasCustomTransform), nil
		},
	}
}

// jsonTextBlock is the per-text-block state of the streaming JSON extractor
// (TS textBlocks[id]).
type jsonTextBlock struct {
	start          *provider.StreamChunk // withheld text-start (nil when the provider sent none)
	phase          string                // prefix | streaming | buffering
	buffer         string
	prefixStripped bool
}

// streamingExtractJSONWrapper mirrors TS extractJsonMiddleware's wrapStream:
// every text block (keyed by chunk ID) is tracked independently, the
// text-start is withheld until the fence prefix is resolved, and whatever is
// still held back is flushed as a final text delta when the block's text-end
// arrives, so the delta always precedes the text-end and finish chunks.
type streamingExtractJSONWrapper struct {
	stream             provider.TextStream
	transform          func(string) string
	hasCustomTransform bool
	blocks             map[string]*jsonTextBlock
	order              []string // block IDs in creation order, for the EOF flush
	pending            []*provider.StreamChunk
	eof                error
}

// newStreamingExtractJSONWrapper creates the block-aware wrapper.
func newStreamingExtractJSONWrapper(
	stream provider.TextStream,
	transform func(string) string,
	hasCustomTransform bool,
) provider.TextStream {
	return &streamingExtractJSONWrapper{
		stream:             stream,
		transform:          transform,
		hasCustomTransform: hasCustomTransform,
		blocks:             map[string]*jsonTextBlock{},
	}
}

func (w *streamingExtractJSONWrapper) newBlock(id string, start *provider.StreamChunk) *jsonTextBlock {
	phase := "prefix"
	if w.hasCustomTransform {
		phase = "buffering"
	}
	b := &jsonTextBlock{start: start, phase: phase}
	w.blocks[id] = b
	w.order = append(w.order, id)
	return b
}

func (w *streamingExtractJSONWrapper) emitStart(b *jsonTextBlock) {
	if b.start != nil {
		w.pending = append(w.pending, b.start)
		b.start = nil
	}
}

func (w *streamingExtractJSONWrapper) emitText(id, text string, template *provider.StreamChunk) {
	c := provider.StreamChunk{}
	if template != nil {
		c = *template
	}
	c.Type = provider.ChunkTypeText
	c.ID = id
	c.Text = text
	w.pending = append(w.pending, &c)
}

// flushBlock emits the remaining buffered text of a block (the TS text-end
// handling).
func (w *streamingExtractJSONWrapper) flushBlock(id string, b *jsonTextBlock) {
	if b.phase == "prefix" || b.phase == "buffering" {
		w.emitStart(b)
	}
	remaining := b.buffer
	switch {
	case b.phase == "buffering":
		remaining = w.transform(remaining)
	case b.prefixStripped:
		remaining = stripMarkdownCodeFenceSuffix(remaining)
	case b.phase == "prefix":
		remaining = w.transform(remaining)
	default:
		remaining = stripMarkdownCodeFenceSuffix(remaining)
	}
	if len(remaining) > 0 {
		w.emitText(id, remaining, nil)
	}
	delete(w.blocks, id)
}

func (w *streamingExtractJSONWrapper) handleDelta(chunk *provider.StreamChunk) {
	b := w.blocks[chunk.ID]
	if b == nil {
		// No text-start seen (providers that do not emit block boundaries):
		// treat the delta as the start of an implicit block.
		b = w.newBlock(chunk.ID, nil)
	}
	b.buffer += chunk.Text

	if b.phase == "buffering" {
		return
	}

	if b.phase == "prefix" {
		switch {
		case len(b.buffer) > 0 && !strings.HasPrefix(b.buffer, "`"):
			b.phase = "streaming"
			w.emitStart(b)
		case strings.HasPrefix(b.buffer, "```"):
			if strings.Contains(b.buffer, "\n") {
				re := regexp.MustCompile(`^` + "```" + `(?:json)?` + jsWS + `*\n`)
				if loc := re.FindStringIndex(b.buffer); loc != nil {
					b.buffer = b.buffer[loc[1]:]
					b.prefixStripped = true
				}
				b.phase = "streaming"
				w.emitStart(b)
			}
		case len(b.buffer) >= 3:
			b.phase = "streaming"
			w.emitStart(b)
		}
	}

	if b.phase == "streaming" {
		potentialSuffixStart := getPotentialSuffixStart(b.buffer)
		toStream := b.buffer[:potentialSuffixStart]
		b.buffer = b.buffer[potentialSuffixStart:]
		if len(toStream) > 0 {
			w.emitText(chunk.ID, toStream, chunk)
		}
	}
}

// Next returns the next chunk with transformations applied.
func (w *streamingExtractJSONWrapper) Next() (*provider.StreamChunk, error) {
	for {
		if len(w.pending) > 0 {
			c := w.pending[0]
			w.pending = w.pending[1:]
			return c, nil
		}
		if w.eof != nil {
			return nil, w.eof
		}

		chunk, err := w.stream.Next()
		if err != nil {
			// Flush blocks that never saw a text-end (including implicit
			// blocks) before reporting the end of the stream.
			for _, id := range w.order {
				if b, ok := w.blocks[id]; ok {
					w.flushBlock(id, b)
				}
			}
			w.eof = err
			continue
		}

		switch chunk.Type {
		case provider.ChunkTypeTextStart:
			c := *chunk
			w.newBlock(chunk.ID, &c)
		case provider.ChunkTypeText:
			w.handleDelta(chunk)
		case provider.ChunkTypeTextEnd:
			if b, ok := w.blocks[chunk.ID]; ok {
				w.flushBlock(chunk.ID, b)
			}
			w.pending = append(w.pending, chunk)
		default:
			w.pending = append(w.pending, chunk)
		}
	}
}

// Close closes the underlying stream
func (w *streamingExtractJSONWrapper) Close() error {
	return w.stream.Close()
}

// Err returns any error from the underlying stream
func (w *streamingExtractJSONWrapper) Err() error {
	return w.stream.Err()
}
