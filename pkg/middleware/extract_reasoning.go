package middleware

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ExtractReasoningOptions configures the reasoning extraction middleware
type ExtractReasoningOptions struct {
	// TagName is the XML tag name to extract reasoning from
	// (e.g., "think" for Anthropic, "reasoning" for OpenAI)
	TagName string

	// Separator is the separator to use between reasoning and text sections
	// Default: "\n"
	Separator string

	// StartWithReasoning indicates whether reasoning tokens appear at the beginning
	// Default: false
	StartWithReasoning bool
}

// ExtractReasoningMiddleware returns middleware that extracts XML-tagged reasoning
// sections from generated text and exposes them as separate reasoning content.
//
// This middleware is useful for models that expose their reasoning process, such as:
// - OpenAI o1 models (use tagName: "reasoning")
// - Anthropic Claude with thinking (use tagName: "think")
//
// Example:
//
//	middleware := ExtractReasoningMiddleware(&ExtractReasoningOptions{
//		TagName:            "think",
//		Separator:          "\n",
//		StartWithReasoning: false,
//	})
//	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)
func ExtractReasoningMiddleware(options *ExtractReasoningOptions) *LanguageModelMiddleware {
	if options == nil {
		options = &ExtractReasoningOptions{
			TagName:            "think",
			Separator:          "\n",
			StartWithReasoning: false,
		}
	}

	if options.Separator == "" {
		options.Separator = "\n"
	}

	openingTag := fmt.Sprintf("<%s>", options.TagName)
	closingTag := fmt.Sprintf("</%s>", options.TagName)

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

			text := result.Text
			if options.StartWithReasoning {
				text = openingTag + text
			}

			// Extract all reasoning blocks
			pattern := fmt.Sprintf(`%s(.*?)%s`, regexp.QuoteMeta(openingTag), regexp.QuoteMeta(closingTag))
			re := regexp.MustCompile(pattern)
			matches := re.FindAllStringSubmatch(text, -1)

			if len(matches) == 0 {
				return result, nil
			}

			// Collect all reasoning text
			reasoningParts := make([]string, len(matches))
			for i, match := range matches {
				if len(match) > 1 {
					reasoningParts[i] = match[1]
				}
			}
			// reasoningText is extracted but not stored in GenerateResult (no field for it yet)
			_ = strings.Join(reasoningParts, options.Separator)

			// Remove reasoning blocks from text
			textWithoutReasoning := text
			for i := len(matches) - 1; i >= 0; i-- {
				match := matches[i]
				matchIndex := strings.Index(textWithoutReasoning, match[0])
				if matchIndex == -1 {
					continue
				}

				beforeMatch := textWithoutReasoning[:matchIndex]
				afterMatch := textWithoutReasoning[matchIndex+len(match[0]):]

				separator := ""
				if len(beforeMatch) > 0 && len(afterMatch) > 0 {
					separator = options.Separator
				}

				textWithoutReasoning = beforeMatch + separator + afterMatch
			}

			// Update result with separated reasoning and text
			// Note: The Go SDK stores reasoning separately but still includes it in Text field
			// for backwards compatibility
			result.Text = textWithoutReasoning

			// Store reasoning in a structured way (if there's a field for it in the future)
			// For now, we've extracted it but the Go SDK doesn't have a separate Reasoning field
			// in GenerateResult. This is primarily useful for streaming where we emit separate chunks.

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

			return &extractReasoningStream{
				underlying:         stream,
				openingTag:         openingTag,
				closingTag:         closingTag,
				separator:          options.Separator,
				startWithReasoning: options.StartWithReasoning,
				extractions:        map[string]*reasoningExtraction{},
				delayedTextStarts:  map[string]*provider.StreamChunk{},
			}, nil
		},
	}
}

// reasoningExtraction holds the per-text-block-ID state machine used to pull
// XML-tagged reasoning out of a single content block's text deltas. Streams
// that interleave multiple text blocks (distinct StreamChunk.ID values, e.g.
// two concurrent text parts) get one independent extraction per ID so their
// buffers, tag-switch state and reasoning IDs never bleed into each other
// (audit row 2b105fa / WG12).
type reasoningExtraction struct {
	isFirstReasoning bool
	isFirstText      bool
	afterSwitch      bool
	isReasoning      bool
	buffer           string
	reasoningID      string
	textID           string
}

// extractReasoningStream wraps a TextStream to extract reasoning from text
// chunks, mirroring TS extractReasoningMiddleware's wrapStream transform.
type extractReasoningStream struct {
	underlying         provider.TextStream
	openingTag         string
	closingTag         string
	separator          string
	startWithReasoning bool

	extractions        map[string]*reasoningExtraction
	reasoningIDCounter int

	// delayedTextStarts holds text-start chunks until either the matching
	// text-end chunk arrives (unused block: forwarded as-is) or the first
	// non-reasoning text-delta is published for that ID (so a text-start
	// never precedes the reasoning-start of a leading reasoning block —
	// see https://github.com/vercel/ai/issues/7774).
	delayedTextStarts map[string]*provider.StreamChunk

	pending []*provider.StreamChunk
}

// Next returns the next chunk from the stream, with reasoning extraction applied.
func (s *extractReasoningStream) Next() (*provider.StreamChunk, error) {
	for {
		if len(s.pending) > 0 {
			chunk := s.pending[0]
			s.pending = s.pending[1:]
			return chunk, nil
		}

		chunk, err := s.underlying.Next()
		if err != nil {
			return chunk, err
		}

		s.transform(*chunk)
	}
}

// enqueue appends chunk to the pending output queue.
func (s *extractReasoningStream) enqueue(chunk provider.StreamChunk) {
	c := chunk
	s.pending = append(s.pending, &c)
}

// transform processes one upstream chunk, appending zero or more output
// chunks to s.pending.
func (s *extractReasoningStream) transform(chunk provider.StreamChunk) {
	// Do not send text-start before reasoning-start.
	if chunk.Type == provider.ChunkTypeTextStart {
		c := chunk
		s.delayedTextStarts[chunk.ID] = &c
		return
	}

	if chunk.Type != provider.ChunkTypeText && chunk.Type != provider.ChunkTypeTextEnd {
		s.enqueue(chunk)
		return
	}

	// Only a text-delta starts a new extraction; a text-end with no prior
	// delta for this ID (activeExtraction == nil in TS) falls through to the
	// "no extraction" branch below instead of creating one.
	if chunk.Type == provider.ChunkTypeText {
		if _, ok := s.extractions[chunk.ID]; !ok {
			s.extractions[chunk.ID] = &reasoningExtraction{
				isFirstReasoning: true,
				isFirstText:      true,
				isReasoning:      s.startWithReasoning,
				textID:           chunk.ID,
			}
		}
	}

	ext, ok := s.extractions[chunk.ID]
	if !ok {
		if start, dOk := s.delayedTextStarts[chunk.ID]; dOk {
			s.enqueue(*start)
			delete(s.delayedTextStarts, chunk.ID)
		}
		s.enqueue(chunk)
		return
	}

	getReasoningID := func() string {
		if ext.reasoningID == "" {
			ext.reasoningID = fmt.Sprintf("reasoning-%d", s.reasoningIDCounter)
			s.reasoningIDCounter++
		}
		return ext.reasoningID
	}

	publish := func(text string) {
		if len(text) == 0 {
			return
		}

		prefix := ""
		if ext.afterSwitch {
			if ext.isReasoning {
				if !ext.isFirstReasoning {
					prefix = s.separator
				}
			} else if !ext.isFirstText {
				prefix = s.separator
			}
		}

		if ext.isReasoning && (ext.afterSwitch || ext.isFirstReasoning) {
			s.enqueue(provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: getReasoningID()})
		}

		if ext.isReasoning {
			s.enqueue(provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: getReasoningID(), Reasoning: prefix + text})
		} else {
			if start, ok := s.delayedTextStarts[ext.textID]; ok {
				s.enqueue(*start)
				delete(s.delayedTextStarts, ext.textID)
			}
			s.enqueue(provider.StreamChunk{Type: provider.ChunkTypeText, ID: ext.textID, Text: prefix + text})
		}
		ext.afterSwitch = false

		if ext.isReasoning {
			ext.isFirstReasoning = false
		} else {
			ext.isFirstText = false
		}
	}

	if chunk.Type == provider.ChunkTypeTextEnd {
		// Flush whatever is left in the buffer instead of silently dropping
		// it: without this, text (or reasoning) ending with a partial
		// opening/closing tag that never resolved into a full match was lost
		// when the block ended.
		publish(ext.buffer)
		ext.buffer = ""
		if start, ok := s.delayedTextStarts[chunk.ID]; ok {
			s.enqueue(*start)
			delete(s.delayedTextStarts, chunk.ID)
		}
		s.enqueue(chunk)
		return
	}

	ext.buffer += chunk.Text

	for {
		nextTag := s.openingTag
		if ext.isReasoning {
			nextTag = s.closingTag
		}

		startIndex := getPotentialStartIndex(ext.buffer, nextTag)

		// No opening or closing tag found: publish the whole buffer.
		if startIndex == -1 {
			publish(ext.buffer)
			ext.buffer = ""
			break
		}

		// Publish text before the tag.
		publish(ext.buffer[:startIndex])

		foundFullMatch := startIndex+len(nextTag) <= len(ext.buffer)

		if foundFullMatch {
			ext.buffer = ext.buffer[startIndex+len(nextTag):]

			if ext.isReasoning {
				// Emit reasoning-start for empty reasoning blocks (no delta
				// was published): startWithReasoning=false, <think></think>
				// (afterSwitch=true), or startWithReasoning=true with an
				// immediate </think> (afterSwitch=false).
				if ext.isFirstReasoning {
					s.enqueue(provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: getReasoningID()})
				}
				s.enqueue(provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: getReasoningID()})
				ext.reasoningID = ""
			}

			ext.isReasoning = !ext.isReasoning
			ext.afterSwitch = true
		} else {
			// Partial match at end of buffer: keep buffering.
			ext.buffer = ext.buffer[startIndex:]
			break
		}
	}
}

// Close closes the underlying stream
func (s *extractReasoningStream) Close() error {
	return s.underlying.Close()
}

// Err returns any error from the underlying stream
func (s *extractReasoningStream) Err() error {
	return s.underlying.Err()
}

// getPotentialStartIndex finds where searchedText could potentially start in text.
// Returns the index of either a complete match or a partial match at the end of text.
// Returns -1 if no match is found.
func getPotentialStartIndex(text, searchedText string) int {
	if len(searchedText) == 0 {
		return -1
	}

	// Check for complete substring match
	if idx := strings.Index(text, searchedText); idx != -1 {
		return idx
	}

	// Check for partial match at the end of text
	// (suffix of text matches prefix of searchedText)
	for i := len(text) - 1; i >= 0; i-- {
		suffix := text[i:]
		if strings.HasPrefix(searchedText, suffix) {
			return i
		}
	}

	return -1
}
