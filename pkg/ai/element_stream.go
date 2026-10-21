package ai

import (
	"context"

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

	// MaxItems is an optional upper bound on the number of elements. When the
	// model streams more, the element stream ends and OnError receives the
	// TS validation error ("elements array must contain at most N items").
	MaxItems *int

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
// Elements follow the TS elementStream rules: the text of each step is parsed
// on its own (a step that emitted other text before the JSON, such as a tool
// step, does not disturb a later step), an element is published once it is
// complete (the element still being streamed is held back), and each element
// is published once across steps.
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
	arr := &arrayOutput[ELEMENT]{elementSchema: opts.ElementSchema, maxItems: opts.MaxItems}

	go func() {
		defer close(ch)
		defer func() {
			if opts.OnComplete != nil {
				opts.OnComplete()
			}
		}()
		closed := result.closedSignal()
		err := streamArrayElements(result, arr.ParsePartialOutput, arr.maxItems, arr.minItems, func(elem ElementStreamResult[ELEMENT]) bool {
			if !sendUnlessClosed(ch, elem, closed) {
				return false
			}
			if opts.OnElement != nil {
				opts.OnElement(elem)
			}
			return true
		})
		if err != nil && opts.OnError != nil {
			opts.OnError(err)
		}
	}()

	return ch
}

// streamArrayElements reads the full stream of result and calls emit for each
// array element as it completes (TS createElementStreamTransform over the
// output transform stream). It returns the stream error, or the maxItems
// validation error when the model exceeds maxItems.
func streamArrayElements[ELEMENT any](
	result *StreamTextResult,
	parsePartial func(context.Context, ParsePartialOutputOptions) (*PartialOutput[[]ELEMENT], error),
	maxItems, minItems *int,
	emit func(ElementStreamResult[ELEMENT]) bool,
) error {
	ctx := context.Background()
	var text string
	published := 0
	// Consume via Stream() rather than result.nextChunk() directly: for
	// StreamTextResult values from StreamText, processStream is already
	// reading the raw provider stream in the background (it always runs
	// now, to execute tools and multi-step continuations regardless of
	// callbacks). Reading result.nextChunk() here too would race with
	// that goroutine over the same underlying stream.
	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if err != nil {
			if err.Error() != "EOF" {
				return err
			}
			return nil
		}
		switch chunk.Type {
		case provider.ChunkTypeStartStep:
			// Output state is per step (TS resets at start-step); the number
			// of published elements is not.
			text = ""
		case provider.ChunkTypeText:
			text += chunk.Text
			partial, perr := parsePartial(ctx, ParsePartialOutputOptions{Text: text})
			if perr != nil || partial == nil {
				continue
			}
			for ; published < len(partial.Partial); published++ {
				if maxItems != nil && published >= *maxItems {
					return arrayLengthValidationError(len(partial.Partial), minItems, maxItems)
				}
				if !emit(ElementStreamResult[ELEMENT]{Element: partial.Partial[published], Index: published}) {
					return nil // consumer abandoned the stream (result closed)
				}
			}
		case provider.ChunkTypeFinish:
			return nil
		}
	}
}

// ElementStreamMethod adds the ElementStream method to StreamTextResult
// This method should be called from within StreamTextResult

// ElementStreamWithOutput creates an element stream from a StreamTextResult using an ArrayOutput
// This is a convenience method that combines output parsing with element streaming.
// It follows the same rules as ElementStream; an output built with
// ArrayOutput also applies its MaxItems bound (the stream ends when the model
// exceeds it).
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

	var maxItems, minItems *int
	if arr, ok := output.(*arrayOutput[ELEMENT]); ok {
		maxItems, minItems = arr.maxItems, arr.minItems
	}

	go func() {
		defer close(ch)
		closed := result.closedSignal()
		_ = streamArrayElements(result, output.ParsePartialOutput, maxItems, minItems, func(elem ElementStreamResult[ELEMENT]) bool {
			return sendUnlessClosed(ch, elem, closed)
		})
	}()

	return ch
}

// sendUnlessClosed sends v on ch, giving up (false) when closed fires first, so
// a consumer that stops reading and closes the result cannot strand the
// producing goroutine.
func sendUnlessClosed[T any](ch chan<- T, v T, closed <-chan struct{}) bool {
	select {
	case ch <- v:
		return true
	case <-closed:
		return false
	}
}
