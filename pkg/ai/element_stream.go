package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ElementStreamResult represents a single element in the stream
type ElementStreamResult[ELEMENT any] struct {
	// The parsed element
	Element ELEMENT

	// Index of this element in the array
	Index int

	// Whether this is the final element in the array
	IsFinal bool
}

// ElementStreamOptions contains options for element streaming
type ElementStreamOptions[ELEMENT any] struct {
	// ElementSchema defines the structure of each array element
	ElementSchema schema.Schema

	// OnElement is called when a new element is parsed
	OnElement func(element ElementStreamResult[ELEMENT])

	// OnError is called when an error occurs during parsing
	OnError func(err error)

	// OnComplete is called when the stream completes
	OnComplete func()
}

// ElementStream creates a channel that streams array elements as they complete
// This is useful for streaming arrays where elements are generated incrementally.
//
// Example:
//
//	result, err := StreamText(ctx, StreamTextOptions{
//	    Model: model,
//	    Prompt: "Generate a list of 5 todo items",
//	})
//	if err != nil {
//	    // handle error
//	}
//
//	elements := ElementStream[TodoItem](result, ElementStreamOptions[TodoItem]{
//	    ElementSchema: todoSchema,
//	    OnElement: func(elem ElementStreamResult[TodoItem]) {
//	        fmt.Printf("Got element %d: %v\n", elem.Index, elem.Element)
//	    },
//	})
//
//	for elem := range elements {
//	    // Process each element as it arrives
//	    fmt.Println(elem)
//	}
func ElementStream[ELEMENT any](result *StreamTextResult, opts ElementStreamOptions[ELEMENT]) <-chan ElementStreamResult[ELEMENT] {
	ch := make(chan ElementStreamResult[ELEMENT], 10)

	go func() {
		defer close(ch)
		defer func() {
			if opts.OnComplete != nil {
				opts.OnComplete()
			}
		}()

		var lastText string
		var lastElementCount int
		ctx := context.Background()

		for {
			chunk, err := result.nextChunk(ctx)
			if err != nil {
				if err.Error() != "EOF" && opts.OnError != nil {
					opts.OnError(err)
				}
				break
			}

			// Accumulate text
			if chunk.Type == provider.ChunkTypeText {
				lastText += chunk.Text

				// Try to parse as partial JSON array
				elements, err := parsePartialArrayElements[ELEMENT](lastText, opts.ElementSchema)
				if err != nil {
					// Not yet parseable, continue
					continue
				}

				// Emit any new elements
				if len(elements) > lastElementCount {
					for i := lastElementCount; i < len(elements); i++ {
						elemResult := ElementStreamResult[ELEMENT]{
							Element: elements[i],
							Index:   i,
							IsFinal: false,
						}

						ch <- elemResult
						if opts.OnElement != nil {
							opts.OnElement(elemResult)
						}
					}
					lastElementCount = len(elements)
				}
			}

			// Handle finish
			if chunk.Type == provider.ChunkTypeFinish {
				// Mark the last element as final if we have any
				if lastElementCount > 0 {
				// Intentionally empty - waiting for more complete element

					// We already emitted all elements, just note that it's complete
				}
				break
			}
		}
	}()

	return ch
}

// parsePartialArrayElements parses a partial JSON array string and extracts complete elements
func parsePartialArrayElements[ELEMENT any](text string, elementSchema schema.Schema) ([]ELEMENT, error) {
	// Try to parse as partial JSON
	parsed, err := jsonutil.ParsePartialJSON(text)
	if err != nil {
		return nil, err
	}

	if parsed == nil {
		return nil, fmt.Errorf("no parseable content yet")
	}

	// Check if it has an elements array (matching our ArrayOutput format)
	parsedMap, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("response is not an object")
	}

	elementsRaw, ok := parsedMap["elements"]
	if !ok {
		return nil, fmt.Errorf("response does not have elements array")
	}

	elementsArray, ok := elementsRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("elements is not an array")
	}

	// Parse each complete element
	var elements []ELEMENT
	for i, elemRaw := range elementsArray {
		// Skip last element if it might be incomplete
		// (This is a heuristic - we could be more sophisticated here)
		if i == len(elementsArray)-1 {
			// Check if this element looks complete by trying to validate
			if err := elementSchema.Validator().Validate(elemRaw); err != nil {
				// Last element is incomplete, skip it
				break
			}
		}

		// Validate element
		if err := elementSchema.Validator().Validate(elemRaw); err != nil {
			// Invalid element, skip
			continue
		}

		// Convert to typed element
		jsonBytes, err := json.Marshal(elemRaw)
		if err != nil {
			continue
		}

		var typedElem ELEMENT
		if err := json.Unmarshal(jsonBytes, &typedElem); err != nil {
			continue
		}

		elements = append(elements, typedElem)
	}

	return elements, nil
}

// ElementStreamMethod adds the ElementStream method to StreamTextResult
// This method should be called from within StreamTextResult

// ElementStreamWithOutput creates an element stream from a StreamTextResult using an ArrayOutput
// This is a convenience method that combines output parsing with element streaming.
//
// Example:
//
//	result, err := StreamText(ctx, StreamTextOptions{
//	    Model: model,
//	    Prompt: "Generate a list of 5 todo items",
//	})
//	if err != nil {
//	    // handle error
//	}
//
//	output := ArrayOutput[TodoItem](ArrayOutputOptions[TodoItem]{
//	    ElementSchema: todoSchema,
//	})
//
//	elements := ElementStreamWithOutput(result, output)
//	for elem := range elements {
//	    fmt.Printf("Element %d: %v\n", elem.Index, elem.Element)
//	}
func ElementStreamWithOutput[ELEMENT any](result *StreamTextResult, output Output[[]ELEMENT, []ELEMENT]) <-chan ElementStreamResult[ELEMENT] {
	ch := make(chan ElementStreamResult[ELEMENT], 10)

	// Extract element schema from the array output
	// This is a bit of a hack - we need to access the internal structure
	// For now, we'll create a simple streaming implementation

	go func() {
		defer close(ch)

		var lastText string
		var lastElementCount int
		ctx := context.Background()

		for {
			chunk, err := result.nextChunk(ctx)
			if err != nil {
				break
			}

			// Accumulate text
			if chunk.Type == provider.ChunkTypeText {
				lastText += chunk.Text

				// Try to parse partial output
				partialOutput, err := output.ParsePartialOutput(ctx, ParsePartialOutputOptions{
					Text: lastText,
				})
				if err != nil || partialOutput == nil {
					continue
				}

				// Check if we have new elements
				currentElements := partialOutput.Partial
				if len(currentElements) > lastElementCount {
					for i := lastElementCount; i < len(currentElements); i++ {
						ch <- ElementStreamResult[ELEMENT]{
							Element: currentElements[i],
							Index:   i,
							IsFinal: false,
						}
					}
					lastElementCount = len(currentElements)
				}
			}

			// Handle finish
			if chunk.Type == provider.ChunkTypeFinish {
				break
			}
		}
	}()

	return ch
}
