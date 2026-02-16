package middleware

import (
	"context"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
	text = regexp.MustCompile(`^` + "```" + `(?:json)?\s*\n?`).ReplaceAllString(text, "")
	// Remove closing fence: ```
	text = regexp.MustCompile(`\n?` + "```" + `\s*$`).ReplaceAllString(text, "")
	return strings.TrimSpace(text)
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

			// Transform the text content
			result.Text = transform(result.Text)
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

const suffixBufferSize = 12

// streamingExtractJSONWrapper wraps a TextStream and handles the final chunk transformation
type streamingExtractJSONWrapper struct {
	stream             provider.TextStream
	transform          func(string) string
	hasCustomTransform bool
	phase              string
	buffer             string
	prefixStripped     bool
	finalized          bool
}

// newStreamingExtractJSONWrapper creates a wrapper that properly handles final transformations
func newStreamingExtractJSONWrapper(
	stream provider.TextStream,
	transform func(string) string,
	hasCustomTransform bool,
) provider.TextStream {
	return &streamingExtractJSONWrapper{
		stream:             stream,
		transform:          transform,
		hasCustomTransform: hasCustomTransform,
		phase:              "prefix",
		buffer:             "",
		prefixStripped:     false,
		finalized:          false,
	}
}

// Next returns the next chunk with transformations applied
func (w *streamingExtractJSONWrapper) Next() (*provider.StreamChunk, error) {
	for {
		chunk, err := w.stream.Next()

		// Handle EOF - apply final transformations
		if err != nil {
			if !w.finalized && len(w.buffer) > 0 {
				w.finalized = true

				var remaining string
				if w.hasCustomTransform {
					remaining = w.transform(w.buffer)
				} else if w.prefixStripped {
					// Strip suffix since prefix was already handled
					remaining = regexp.MustCompile(`\n?` + "```" + `\s*$`).ReplaceAllString(w.buffer, "")
					remaining = strings.TrimRight(remaining, " \t\n\r")
				} else {
					// Apply full transform
					remaining = w.transform(w.buffer)
				}

				if len(remaining) > 0 {
					// Return final text chunk before the error
					w.buffer = ""
					return &provider.StreamChunk{
						Type: provider.ChunkTypeText,
						Text: remaining,
					}, nil
				}
			}
			return chunk, err
		}

		// Pass through non-text chunks
		if chunk.Type != provider.ChunkTypeText {
			return chunk, nil
		}

		// Buffer text chunks
		w.buffer += chunk.Text

		// Custom transform: buffer everything
		if w.hasCustomTransform {
			continue
		}

		// Prefix detection phase
		if w.phase == "prefix" {
			if len(w.buffer) > 0 && !strings.HasPrefix(w.buffer, "`") {
				w.phase = "streaming"
			} else if strings.HasPrefix(w.buffer, "```") {
				if strings.Contains(w.buffer, "\n") {
					re := regexp.MustCompile(`^` + "```" + `(?:json)?\s*\n`)
					if loc := re.FindStringIndex(w.buffer); loc != nil {
						w.buffer = w.buffer[loc[1]:]
						w.prefixStripped = true
						w.phase = "streaming"
					} else {
						w.phase = "streaming"
					}
				}
			} else if len(w.buffer) >= 3 {
				w.phase = "streaming"
			}
		}

		// Stream with suffix buffering
		if w.phase == "streaming" && len(w.buffer) > suffixBufferSize {
			toStream := w.buffer[:len(w.buffer)-suffixBufferSize]
			w.buffer = w.buffer[len(w.buffer)-suffixBufferSize:]
			return &provider.StreamChunk{
				Type: provider.ChunkTypeText,
				Text: toStream,
			}, nil
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
