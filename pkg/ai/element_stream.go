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
		err := streamArrayElements(result, nil, arr, arr.ParsePartialOutput, arr.maxItems, func(elem ElementStreamResult[ELEMENT]) bool {
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

// streamArrayElements calls emit for each array element as it completes (TS
// createElementStreamTransform over the partial outputs of
// createOutputTransformStream). For the call's own Output (logOutput) it reads
// the partial outputs StreamText published (parsed incrementally from the
// step's first text block, reset at step and stream retry boundaries,
// 91a67c2f56). Otherwise it feeds the full stream through the same output
// stage with spec; an output that is not one of this package's falls back to
// parsePartial over the step's text. It returns the stream error, or the
// maxItems validation error when the model exceeds maxItems.
func streamArrayElements[ELEMENT any](
	result *StreamTextResult,
	logOutput any,
	spec outputProcessor,
	parsePartial func(context.Context, ParsePartialOutputOptions) (*PartialOutput[[]ELEMENT], error),
	maxItems *int,
	emit func(ElementStreamResult[ELEMENT]) bool,
) error {
	ctx := context.Background()
	published := 0
	// publish emits the elements of partial not published yet; false ends the
	// stream (consumer gone, or maxItems exceeded with *err set).
	publish := func(partial []ELEMENT, err *error) bool {
		for ; published < len(partial); published++ {
			if maxItems != nil && published >= *maxItems {
				// TS createElementStreamTransform errors with
				// getArrayLengthValidationError({value: partialOutput,
				// maxItems}): a TypeValidationError holding the partial
				// array (maxItems only).
				lengthErr := arrayLengthValidationError(len(partial), nil, maxItems)
				*err = objectTypeValidationError(partial, lengthErr.Error(), lengthErr)
				return false
			}
			if !emit(ElementStreamResult[ELEMENT]{Element: partial[published], Index: published}) {
				return false // consumer abandoned the stream (result closed)
			}
		}
		return true
	}

	if log := result.partialOutputLog(logOutput); log != nil {
		closed := result.closedSignal()
		finished := make(chan struct{})
		defer close(finished)
		go func() {
			select {
			case <-closed:
				log.wake()
			case <-finished:
			}
		}()
		isClosed := func() bool {
			select {
			case <-closed:
				return true
			default:
				return false
			}
		}
		for i := 0; ; i++ {
			v, ok, logErr := log.next(i, isClosed)
			if !ok {
				return logErr
			}
			partial, isArray := v.([]ELEMENT)
			if !isArray {
				continue
			}
			var err error
			if !publish(partial, &err) {
				return err
			}
		}
	}

	var tracker partialOutputTracker
	var text string
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
		if chunk.Type == provider.ChunkTypeFinish {
			return nil
		}
		var partial []ELEMENT
		if spec != nil {
			// The output stage keeps its state per step (TS resets at
			// start-step) and reads the step's first text block only; the
			// number of published elements is not reset.
			v, ok, perr := tracker.observe(ctx, spec, *chunk)
			if perr != nil || !ok {
				continue
			}
			if partial, ok = v.([]ELEMENT); !ok {
				continue
			}
		} else {
			switch chunk.Type {
			case provider.ChunkTypeStartStep:
				text = ""
				continue
			case provider.ChunkTypeText:
				text += chunk.Text
			default:
				continue
			}
			p, perr := parsePartial(ctx, ParsePartialOutputOptions{Text: text})
			if perr != nil || p == nil {
				continue
			}
			partial = p.Partial
		}
		var emitErr error
		if !publish(partial, &emitErr) {
			return emitErr
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

	var maxItems *int
	if arr, ok := output.(*arrayOutput[ELEMENT]); ok {
		maxItems = arr.maxItems
	}

	go func() {
		defer close(ch)
		closed := result.closedSignal()
		spec, _ := output.(outputProcessor)
		_ = streamArrayElements(result, output, spec, output.ParsePartialOutput, maxItems, func(elem ElementStreamResult[ELEMENT]) bool {
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
