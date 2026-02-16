package middleware

import (
	"context"
	"fmt"
	"io"
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
				isReasoning:        options.StartWithReasoning,
				isFirstReasoning:   true,
				isFirstText:        true,
				buffer:             "",
			}, nil
		},
	}
}

// extractReasoningStream wraps a TextStream to extract reasoning from text chunks
type extractReasoningStream struct {
	underlying         provider.TextStream
	openingTag         string
	closingTag         string
	separator          string
	startWithReasoning bool
	isReasoning        bool
	isFirstReasoning   bool
	isFirstText        bool
	afterSwitch        bool
	buffer             string
	reasoningCounter   int
}

// Next returns the next chunk from the stream, with reasoning extraction applied
func (s *extractReasoningStream) Next() (*provider.StreamChunk, error) {
	for {
		chunk, err := s.underlying.Next()
		if err != nil {
			// Flush remaining buffer on EOF
			if err == io.EOF && len(s.buffer) > 0 {
				bufferedChunk := s.flushBuffer()
				if bufferedChunk != nil {
					s.buffer = ""
					return bufferedChunk, nil
				}
			}
			return chunk, err
		}

		// Pass through non-text chunks unchanged
		if chunk.Type != provider.ChunkTypeText {
			return chunk, nil
		}

		// Buffer the incoming text
		s.buffer += chunk.Text

		// Process buffer to extract reasoning/text
		for {
			nextTag := s.closingTag
			if !s.isReasoning {
				nextTag = s.openingTag
			}

			startIndex := getPotentialStartIndex(s.buffer, nextTag)

			// No tag found, publish the buffer
			if startIndex == -1 {
				if len(s.buffer) > 0 {
					publishChunk := s.createChunk(s.buffer)
					s.buffer = ""
					if publishChunk != nil {
						return publishChunk, nil
					}
				}
				break
			}

			// Publish text before the tag
			if startIndex > 0 {
				beforeTag := s.buffer[:startIndex]
				publishChunk := s.createChunk(beforeTag)
				s.buffer = s.buffer[startIndex:]
				if publishChunk != nil {
					return publishChunk, nil
				}
			}

			// Check if we have a complete tag match
			foundFullMatch := startIndex+len(nextTag) <= len(s.buffer)

			if foundFullMatch {
				// Remove the tag from buffer
				s.buffer = s.buffer[len(nextTag):]

				// Switch between reasoning and text mode
				if s.isReasoning {
					s.reasoningCounter++
				}
				s.isReasoning = !s.isReasoning
				s.afterSwitch = true
			} else {
				// Partial match at end of buffer, keep buffering
				break
			}
		}
	}
}

// createChunk creates a chunk with appropriate type and content
func (s *extractReasoningStream) createChunk(text string) *provider.StreamChunk {
	if len(text) == 0 {
		return nil
	}

	// In streaming mode, don't add separators - each section is a separate chunk
	// The separator is only used in non-streaming mode when combining sections
	s.afterSwitch = false

	if s.isReasoning {
		return &provider.StreamChunk{
			Type:      provider.ChunkTypeReasoning,
			Reasoning: text,
		}
	}

	return &provider.StreamChunk{
		Type: provider.ChunkTypeText,
		Text: text,
	}
}

// flushBuffer creates a final chunk from any remaining buffer content
func (s *extractReasoningStream) flushBuffer() *provider.StreamChunk {
	return s.createChunk(s.buffer)
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
