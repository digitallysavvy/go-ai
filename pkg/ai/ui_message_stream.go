package ai

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/intsafe"
	"github.com/digitallysavvy/go-ai/pkg/jsonparser"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// UIMessageChunk is a lightweight JSON-compatible chunk shape.
type UIMessageChunk map[string]interface{}

// UIMessageStreamOnStepFinishCallback is invoked when a logical step finishes.
type UIMessageStreamOnStepFinishCallback func(ctx map[string]interface{})

// UIMessageStreamOnStepEndCallback is the canonical name for UIMessageStreamOnStepFinishCallback.
type UIMessageStreamOnStepEndCallback = UIMessageStreamOnStepFinishCallback

// UIMessageStreamOnEndCallback is invoked when the UI stream ends.
type UIMessageStreamOnEndCallback func(ctx map[string]interface{})

// UIMessageStreamOnFinishCallback is invoked when the UI stream ends.
//
// Deprecated: use UIMessageStreamOnEndCallback.
type UIMessageStreamOnFinishCallback = UIMessageStreamOnEndCallback

// UIMessageStreamOutcomeStatus is the operation-level status of a UI message
// stream. Mirrors TS UIMessageStreamOutcome['status'].
type UIMessageStreamOutcomeStatus string

const (
	UIMessageStreamOutcomeCompleted UIMessageStreamOutcomeStatus = "completed"
	UIMessageStreamOutcomeFailed    UIMessageStreamOutcomeStatus = "failed"
	UIMessageStreamOutcomeAborted   UIMessageStreamOutcomeStatus = "aborted"
	UIMessageStreamOutcomeUnknown   UIMessageStreamOutcomeStatus = "unknown"
)

// UIMessageStreamOutcome is the operation-level outcome of a UI message
// stream. This is separate from model finish reasons and individual stream
// chunks. Fatal stream-processing failures override outcomes declared by the
// stream owner. Consumer cancellation before an outcome is declared keeps
// the "unknown" status and is reported separately through the end
// callback's "isCancelled" field. Mirrors TS UIMessageStreamOutcome.
type UIMessageStreamOutcome struct {
	Status UIMessageStreamOutcomeStatus
	// Error is set when Status is UIMessageStreamOutcomeFailed.
	Error error
}

func uiMessageStreamOutcomeMap(outcome UIMessageStreamOutcome) map[string]interface{} {
	m := map[string]interface{}{"status": string(outcome.Status)}
	if outcome.Status == UIMessageStreamOutcomeFailed && outcome.Error != nil {
		m["error"] = outcome.Error
	}
	return m
}

// UIMessageStreamWriter is used by CreateUIMessageStreamWithOptions to write chunks.
type UIMessageStreamWriter struct {
	writeFn      func(UIMessageChunk)
	mergeFn      func(<-chan UIMessageChunk)
	onError      func(error) string
	setOutcomeFn func(UIMessageStreamOutcome)
}

// Write appends a data stream part.
func (w UIMessageStreamWriter) Write(part UIMessageChunk) {
	if w.writeFn != nil {
		w.writeFn(part)
	}
}

// Merge forwards all chunks from another stream into this stream. Merge
// stops reading stream once the outer stream's ctx is done, but -- unlike TS
// createUIMessageStream's writer.merge(), which holds a ReadableStream
// reader it can call .cancel() on to interrupt the source's own pending pull
// -- a Go channel has no generic way to signal a producer to stop (TS
// #21728). The caller is responsible for making stream's producer honor the
// same ctx (or one derived from it) for its own sends, the way every
// built-in producer in this package does (e.g. ToUIMessageStream's
// `select { case out <- part: case <-ctx.Done(): }`), or that producer's
// goroutine will block forever trying to send once Merge stops receiving.
func (w UIMessageStreamWriter) Merge(stream <-chan UIMessageChunk) {
	if w.mergeFn != nil {
		w.mergeFn(stream)
	}
}

// SetOutcome declares the operation-level outcome of the stream. The first
// non-unknown outcome wins; a fatal processing failure (an Execute panic or
// a merged stream error) always overrides it. Mirrors TS
// createUIMessageStream's `writer.setOutcome`.
func (w UIMessageStreamWriter) SetOutcome(outcome UIMessageStreamOutcome) {
	if w.setOutcomeFn != nil {
		w.setOutcomeFn(outcome)
	}
}

// UIMessageStreamOptions configures custom UI-message stream creation.
type UIMessageStreamOptions struct {
	Execute          func(writer UIMessageStreamWriter)
	OnError          func(error) string
	OriginalMessages []UIMessageChunk
	OnStepEnd        UIMessageStreamOnStepEndCallback
	// Deprecated: use OnStepEnd.
	OnStepFinish UIMessageStreamOnStepFinishCallback
	OnEnd        UIMessageStreamOnEndCallback
	// Deprecated: use OnEnd.
	OnFinish          UIMessageStreamOnFinishCallback
	GenerateMessageID IDGenerator
}

// UIMessageStreamResultOptions controls result-to-UI projection settings.
type UIMessageStreamResultOptions struct {
	OriginalMessages  []UIMessageChunk
	GenerateMessageID IDGenerator
	ResponseMessageID string
	Tools             []types.Tool
	MessageMetadata   func(part map[string]interface{}) map[string]interface{}
	SendReasoning     *bool
	SendSources       *bool
	SendStart         *bool
	SendFinish        *bool
	OnStepEnd         UIMessageStreamOnStepEndCallback
	// Deprecated: use OnStepEnd.
	OnStepFinish UIMessageStreamOnStepFinishCallback
	OnEnd        UIMessageStreamOnEndCallback
	// Deprecated: use OnEnd.
	OnFinish UIMessageStreamOnFinishCallback
	OnError  func(error) string
	// PartialStream marks a source that may end without a finish or error
	// chunk because the producer intentionally suspended it mid-step (a
	// time-sliced harness turn). The converter then forwards what arrived and
	// stops: it does not close open parts, invent a trailing `finish-step` or
	// `finish`, or report an incomplete-stream error. TS forwards stream parts
	// as they come and repairs nothing.
	PartialStream bool
}

// UIMessageStreamResponseInit mirrors the TypeScript response init shape used by
// createUIMessageStreamResponse.
type UIMessageStreamResponseInit struct {
	Status     int
	StatusText string
	// Headers sets a single value per header key. Use Header instead when a
	// header (e.g. Set-Cookie) needs multiple values.
	Headers map[string]string
	// Header carries repeated header values (e.g. multiple Set-Cookie
	// entries), which map[string]string cannot represent. Values here are
	// added in addition to Headers, so both can be set together.
	Header           http.Header
	ConsumeSSEStream func(io.Reader) error
	// KeepAliveMs, when set, makes the SSE pipeline flush an opening
	// ": stream-open\n\n" comment immediately (so headers reach the client
	// right away even if the first real chunk is slow) and send a
	// ": keep-alive\n\n" comment whenever this duration elapses without any
	// other write, resetting on every chunk. Must be a positive duration;
	// zero, a negative duration or one above 2147483647ms is an error, as in
	// TS createSseStreamWithKeepAlive's `keepAliveMs` option (TS #21672).
	KeepAliveMs *time.Duration //nolint:revive,staticcheck // name matches TS SDK field "keepAliveMs" for parity
}

const (
	sseStreamOpenComment = ": stream-open\n\n"
	sseKeepAliveComment  = ": keep-alive\n\n"
)

func getDefaultMessageErrorHandler(onError func(error) string) func(error) string {
	if onError == nil {
		return func(err error) string {
			return "An error occurred."
		}
	}
	return onError
}

func boolOption(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}

func appendErrorChunk(onError func(error) string, write func(UIMessageChunk), err error) {
	if err == nil {
		return
	}
	write(UIMessageChunk{
		"type":      "error",
		"errorText": onError(err),
	})
}

func resolveUIMessageStreamOnStepEnd(onStepEnd, onStepFinish UIMessageStreamOnStepFinishCallback) UIMessageStreamOnStepFinishCallback {
	if onStepEnd != nil {
		return onStepEnd
	}
	return onStepFinish
}

func resolveUIMessageStreamOnEnd(onEnd, onFinish UIMessageStreamOnEndCallback) UIMessageStreamOnEndCallback {
	if onEnd != nil {
		return onEnd
	}
	return onFinish
}

func panicToError(r interface{}) error {
	if err, ok := r.(error); ok {
		return err
	}
	return fmt.Errorf("%v", r)
}

// reportUIStreamCallbackFailure fails the stream with what an onEnd callback
// panicked with. errCh holds one error; the first failure wins.
func reportUIStreamCallbackFailure(errCh chan<- error, r interface{}) {
	select {
	case errCh <- panicToError(r):
	default:
	}
}

// sendUIStreamErr fails the stream with err unless an earlier failure (for
// example a step callback whose onError panicked) already filled errCh, so
// the stream goroutine never blocks on the one-slot channel and out closes.
func sendUIStreamErr(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	default:
	}
}

// CreateUIMessageStreamWithOptions creates a UI message stream with writer callbacks.
func CreateUIMessageStreamWithOptions(ctx context.Context, options UIMessageStreamOptions) (<-chan UIMessageChunk, <-chan error) {
	out := make(chan UIMessageChunk)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)

		if options.Execute == nil {
			errCh <- fmt.Errorf("execute is required")
			return
		}

		onError := getDefaultMessageErrorHandler(options.OnError)

		var mu sync.Mutex
		var closed bool
		safeEnqueue := func(part UIMessageChunk) {
			mu.Lock()
			isClosed := closed
			mu.Unlock()
			if isClosed {
				return
			}
			defer func() {
				_ = recover()
			}()
			select {
			case out <- part:
			case <-ctx.Done():
			}
		}
		var outcomeMu sync.Mutex
		hasFatalFailure := false
		var fatalErr error
		declaredOutcome := UIMessageStreamOutcome{Status: UIMessageStreamOutcomeUnknown}
		setOutcome := func(newOutcome UIMessageStreamOutcome) {
			outcomeMu.Lock()
			defer outcomeMu.Unlock()
			if declaredOutcome.Status == UIMessageStreamOutcomeUnknown && newOutcome.Status != UIMessageStreamOutcomeUnknown {
				declaredOutcome = newOutcome
			}
		}
		failOutcome := func(err error) {
			outcomeMu.Lock()
			defer outcomeMu.Unlock()
			hasFatalFailure = true
			fatalErr = err
		}

		generateID := options.GenerateMessageID
		if generateID == nil {
			// TS default: the built-in generateId (16 alphanumerics, no prefix).
			generateID = GenerateID
		}
		uiState := newUIMessageCallbackState(options.OriginalMessages, generateID())
		// TS handleUIMessageStreamFinish fixes the injected id up front: the
		// continued assistant message's id, else the generated one. A start
		// chunk that carries its own id does not change it.
		responseMessageID := stringValue(uiState.message["id"])

		onEnd := resolveUIMessageStreamOnEnd(options.OnEnd, options.OnFinish)
		// processingErr is the first chunk the callback state could not apply
		// (TS processUIMessageStream throws a UIMessageStreamError). Only set
		// on the consumer goroutine, read after it is done.
		var processingErr error
		callOnEnd := func() {
			if onEnd == nil {
				return
			}
			outcomeMu.Lock()
			outcome := declaredOutcome
			if processingErr != nil {
				// TS handleUIMessageStreamFinish: a processing failure is the
				// outcome, whatever the stream owner declared.
				outcome = UIMessageStreamOutcome{Status: UIMessageStreamOutcomeFailed, Error: processingErr}
			} else if hasFatalFailure {
				outcome = UIMessageStreamOutcome{Status: UIMessageStreamOutcomeFailed, Error: fatalErr}
			} else if outcome.Status == UIMessageStreamOutcomeUnknown && uiState.isAborted {
				outcome = UIMessageStreamOutcome{Status: UIMessageStreamOutcomeAborted}
			}
			outcomeMu.Unlock()
			isCancelled := ctx.Err() != nil && outcome.Status == UIMessageStreamOutcomeUnknown
			finishEvent := map[string]interface{}{
				"isContinuation":  uiState.continuation(),
				"isAborted":       uiState.isAborted || outcome.Status == UIMessageStreamOutcomeAborted,
				"responseMessage": uiState.responseMessage(),
				"messages":        uiState.messages(),
				"finishReason":    uiState.finishReason,
				"outcome":         uiMessageStreamOutcomeMap(outcome),
			}
			if isCancelled {
				finishEvent["isCancelled"] = true
			}
			// TS handleUIMessageStreamFinish awaits onEnd in the transform's
			// flush/cancel, so an error it throws fails the stream.
			defer func() {
				if r := recover(); r != nil {
					reportUIStreamCallbackFailure(errCh, r)
				}
			}()
			onEnd(finishEvent)
		}

		onStepEnd := resolveUIMessageStreamOnStepEnd(options.OnStepEnd, options.OnStepFinish)
		callOnStepFinish := func() {
			if onStepEnd == nil {
				return
			}
			stepEvent := map[string]interface{}{
				"isContinuation":  uiState.continuation(),
				"responseMessage": uiState.responseMessage(),
				"messages":        uiState.messages(),
			}
			// TS callOnStepFinish catches what onStepEnd throws and reports it
			// through the stream's onError; the stream keeps going.
			defer func() {
				if r := recover(); r != nil {
					defer func() {
						if r2 := recover(); r2 != nil {
							reportUIStreamCallbackFailure(errCh, r2)
						}
					}()
					onError(panicToError(r))
				}
			}()
			onStepEnd(stepEvent)
		}
		// TS handleUIMessageStreamFinish only processes the chunks (message
		// state, step callbacks) when an end or step-end callback is set.
		processing := onEnd != nil || onStepEnd != nil
		processAndEnqueue := func(part UIMessageChunk) {
			if processingErr != nil {
				// TS: the processing failure errored the stream.
				return
			}
			// TS handleUIMessageStreamFinish: a start chunk without a
			// messageId gets the response message id (the continued
			// assistant message's id, else the generated one).
			if part["type"] == "start" {
				// TS: `startChunk.messageId == null && messageId != null`.
				if part["messageId"] == nil && responseMessageID != "" {
					injected := make(UIMessageChunk, len(part)+1)
					for k, v := range part {
						injected[k] = v
					}
					injected["messageId"] = responseMessageID
					part = injected
				}
			}
			if processing {
				if err := applyUIStateChunk(uiState, part, onError); err != nil {
					processingErr = err
					sendUIStreamErr(errCh, err)
					return
				}
				if part["type"] == "finish-step" {
					callOnStepFinish()
				}
			}
			safeEnqueue(part)
		}

		// chunkCh fans every chunk -- whether written directly by Execute via
		// writer.Write, or forwarded by a writer.Merge goroutine -- through a
		// single serialized consumer below. uiState's activeText/
		// activeReasoning/partialTools maps are only ever mutated from that
		// one goroutine, matching TS's single-pipeline processing of merged
		// streams and eliminating the concurrent map writes that otherwise
		// occur when Execute merges more than one stream (R1-1).
		chunkCh := make(chan UIMessageChunk)
		consumerDone := make(chan struct{})
		go func() {
			defer close(consumerDone)
			for chunk := range chunkCh {
				processAndEnqueue(chunk)
			}
		}()
		// chunkClosed guards chunkCh: a writer or merge goroutine the caller
		// keeps running after Execute returns must not send on the closed
		// channel. Late chunks are dropped, as safeEnqueue already does for
		// the output stream (and as TS's safeEnqueue ignores them).
		var chunkMu sync.RWMutex
		chunkClosed := false
		sendChunk := func(part UIMessageChunk) {
			chunkMu.RLock()
			defer chunkMu.RUnlock()
			if chunkClosed {
				return
			}
			select {
			case chunkCh <- part:
			case <-ctx.Done():
			}
		}

		// enqueueError writes the error chunk through the same pipeline as
		// every other chunk (TS handleError enqueues it on the stream that
		// handleUIMessageStreamFinish processes).
		enqueueError := func(err error) {
			failOutcome(err)
			// TS handleError: an onError that throws fails the stream with
			// that error instead of writing an error chunk.
			defer func() {
				if r := recover(); r != nil {
					onErrorErr := panicToError(r)
					failOutcome(onErrorErr)
					reportUIStreamCallbackFailure(errCh, r)
				}
			}()
			appendErrorChunk(onError, sendChunk, err)
		}

		var wg sync.WaitGroup
		merge := func(stream <-chan UIMessageChunk) {
			if stream == nil {
				enqueueError(errors.New("merge stream is nil"))
				return
			}
			chunkMu.RLock()
			lateMerge := chunkClosed
			chunkMu.RUnlock()
			if lateMerge {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						if err, ok := r.(error); ok {
							enqueueError(err)
						} else {
							enqueueError(fmt.Errorf("ui message stream panicked: %v", r))
						}
					}
				}()
				for {
					// Check ctx.Done() first (non-blocking) so a consumer
					// cancellation that raced with a chunk already sitting
					// on stream wins immediately, instead of Go's select
					// picking between two simultaneously-ready cases at
					// random — mirrors TS createUIMessageStream's merge
					// fix, where cancelling the reader always stops a
					// pending read before it can resolve with another
					// value (TS #21728).
					select {
					case <-ctx.Done():
						return
					default:
					}
					select {
					case <-ctx.Done():
						return
					case chunk, ok := <-stream:
						if !ok {
							return
						}
						sendChunk(chunk)
					}
				}
			}()
		}

		writer := UIMessageStreamWriter{
			writeFn:      sendChunk,
			mergeFn:      merge,
			onError:      onError,
			setOutcomeFn: setOutcome,
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					var err error
					if e, ok := r.(error); ok {
						err = e
					} else {
						err = fmt.Errorf("ui message stream panicked: %v", r)
					}
					enqueueError(err)
				}
			}()
			options.Execute(writer)
		}()

		wg.Wait()
		chunkMu.Lock()
		chunkClosed = true
		close(chunkCh)
		chunkMu.Unlock()
		<-consumerDone

		mu.Lock()
		closed = true
		mu.Unlock()

		callOnEnd()
	}()

	return out, errCh
}

// CreateUIMessageStream converts a StreamTextResult into a channel of UI chunks.
func CreateUIMessageStream(ctx context.Context, result *StreamTextResult, opts ...UIMessageStreamResultOptions) (<-chan UIMessageChunk, <-chan error) {
	if result == nil {
		out := make(chan UIMessageChunk)
		errCh := make(chan error, 1)
		close(out)
		errCh <- fmt.Errorf("result is required")
		close(errCh)
		return out, errCh
	}
	return ToUIMessageStream(ctx, result.uiStream(), result.uiOptionsWithRequestTools(opts)...)
}

// uiOptionsWithRequestTools defaults the UI conversion's Tools to the tool set
// the call was made with (TS toUIMessageStream passes `this.tools`), unless
// the caller supplied its own.
func (r *StreamTextResult) uiOptionsWithRequestTools(opts []UIMessageStreamResultOptions) []UIMessageStreamResultOptions {
	if len(r.requestTools) == 0 || (len(opts) > 0 && len(opts[0].Tools) > 0) {
		return opts
	}
	var options UIMessageStreamResultOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	options.Tools = r.requestTools
	return append([]UIMessageStreamResultOptions{options}, opts[min(len(opts), 1):]...)
}

// ToUIMessageStream converts a provider text stream into UI message chunks.
func ToUIMessageStream(ctx context.Context, stream provider.TextStream, opts ...UIMessageStreamResultOptions) (<-chan UIMessageChunk, <-chan error) {
	out := make(chan UIMessageChunk)
	errCh := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errCh)
		// An OnError, MessageMetadata or conversion callback that panics
		// fails the stream (TS: an error thrown in the transform errors the
		// stream) instead of crashing the program from this goroutine, and
		// OnEnd runs once with that error as the failed outcome (TS
		// failOutcome, then handleUIMessageStreamFinish's cancel).
		var endOnPanic func(error)
		defer func() {
			if r := recover(); r != nil {
				reportUIStreamCallbackFailure(errCh, r)
				if endOnPanic != nil {
					endOnPanic(panicToError(r))
				}
			}
		}()
		if stream == nil {
			errCh <- fmt.Errorf("stream is required")
			return
		}

		options := UIMessageStreamResultOptions{}
		if len(opts) > 0 {
			options = opts[0]
		}

		onError := getDefaultMessageErrorHandler(options.OnError)
		sendStart := boolOption(options.SendStart, true)
		sendFinish := boolOption(options.SendFinish, true)
		sendReasoning := boolOption(options.SendReasoning, true)

		streamMessageID := responseUIMessageID(options)
		callbackMessageID := streamMessageID
		if callbackMessageID == "" && options.GenerateMessageID != nil {
			callbackMessageID = options.GenerateMessageID()
		}

		uiState := newUIMessageCallbackState(options.OriginalMessages, callbackMessageID)

		// sourceOutcome mirrors TS to-ui-message-stream.ts's setSourceOutcome/
		// failOutcome: derives the operation-level outcome from the source
		// stream's own finish/abort/error parts (audit row #103).
		sourceOutcome := UIMessageStreamOutcome{Status: UIMessageStreamOutcomeUnknown}
		hasFatalOutcomeFailure := false
		setSourceOutcome := func(newOutcome UIMessageStreamOutcome) {
			if hasFatalOutcomeFailure ||
				sourceOutcome.Status == UIMessageStreamOutcomeCompleted ||
				sourceOutcome.Status == UIMessageStreamOutcomeAborted ||
				newOutcome.Status == UIMessageStreamOutcomeUnknown ||
				(sourceOutcome.Status != UIMessageStreamOutcomeUnknown && newOutcome.Status == UIMessageStreamOutcomeFailed) {
				return
			}
			sourceOutcome = newOutcome
		}
		failSourceOutcome := func(err error) {
			hasFatalOutcomeFailure = true
			sourceOutcome = UIMessageStreamOutcome{Status: UIMessageStreamOutcomeFailed, Error: err}
		}

		onEnd := resolveUIMessageStreamOnEnd(options.OnEnd, options.OnFinish)
		// processingErr is the first chunk the callback state could not apply
		// (TS processUIMessageStream throws a UIMessageStreamError, which
		// errors the stream and becomes the outcome).
		var processingErr error
		endCalled := false
		callOnEnd := func() {
			if onEnd == nil || endCalled {
				return
			}
			endCalled = true
			// TS handleUIMessageStreamFinish passes state.finishReason: the
			// reason of the last finish chunk the message state applied, so
			// none when SendFinish is false or the stream failed before its
			// finish chunk.
			finishReason := types.FinishReason("")
			switch reason := uiState.finishReason.(type) {
			case string:
				finishReason = types.FinishReason(reason)
			case types.FinishReason:
				finishReason = reason
			}
			if processingErr != nil {
				sourceOutcome = UIMessageStreamOutcome{Status: UIMessageStreamOutcomeFailed, Error: processingErr}
			}
			// isCancelled reflects the consumer stopping (ctx cancelled) before
			// the source stream itself declared an outcome; isAborted reflects
			// the source stream's own abort part. Mirrors CreateUIMessageStream
			// above and TS to-ui-message-stream.ts (audit row #10/#103).
			isCancelled := ctx.Err() != nil && sourceOutcome.Status == UIMessageStreamOutcomeUnknown
			finishEvent := map[string]interface{}{
				"isContinuation":  uiState.continuation(),
				"isAborted":       uiState.isAborted || sourceOutcome.Status == UIMessageStreamOutcomeAborted,
				"responseMessage": uiState.responseMessage(),
				"messages":        uiState.messages(),
				"finishReason":    finishReason,
				"outcome":         uiMessageStreamOutcomeMap(sourceOutcome),
			}
			if isCancelled {
				finishEvent["isCancelled"] = true
			}
			// TS handleUIMessageStreamFinish awaits onEnd in the transform's
			// flush/cancel, so an error it throws fails the stream.
			defer func() {
				if r := recover(); r != nil {
					reportUIStreamCallbackFailure(errCh, r)
				}
			}()
			onEnd(finishEvent)
		}
		finishReason := types.FinishReason("")
		endOnPanic = func(err error) {
			failSourceOutcome(err)
			callOnEnd()
		}

		onStepEnd := resolveUIMessageStreamOnStepEnd(options.OnStepEnd, options.OnStepFinish)
		callOnStepFinish := func() {
			if onStepEnd == nil {
				return
			}
			stepEvent := map[string]interface{}{
				"isContinuation":  uiState.continuation(),
				"responseMessage": uiState.responseMessage(),
				"messages":        uiState.messages(),
			}
			// TS callOnStepFinish catches what onStepEnd throws and reports it
			// through the stream's onError; the stream keeps going.
			defer func() {
				if r := recover(); r != nil {
					defer func() {
						if r2 := recover(); r2 != nil {
							reportUIStreamCallbackFailure(errCh, r2)
						}
					}()
					onError(panicToError(r))
				}
			}()
			onStepEnd(stepEvent)
		}

		safeEnqueue := func(part UIMessageChunk) {
			select {
			case <-ctx.Done():
				return
			case out <- part:
			}
		}

		// TS handleUIMessageStreamFinish only processes the chunks (message
		// state, step callbacks) when an end or step-end callback is set.
		processing := onEnd != nil || onStepEnd != nil
		processChunk := func(chunk UIMessageChunk) {
			if processingErr != nil {
				return
			}
			if processing {
				if err := applyUIStateChunk(uiState, chunk, onError); err != nil {
					processingErr = err
					sendUIStreamErr(errCh, err)
					return
				}
				if chunk["type"] == "finish-step" {
					callOnStepFinish()
				}
			}
			safeEnqueue(chunk)
		}
		messageMetadataInput := func(part provider.StreamChunk, overrideType string) map[string]interface{} {
			partType := string(part.Type)
			if overrideType != "" {
				partType = overrideType
			} else {
				switch part.Type {
				case provider.ChunkTypeStreamStart:
					partType = "start-step"
				case provider.ChunkTypeFinish, provider.ChunkTypeStreamFinish:
					partType = "finish-step"
				case provider.ChunkTypeText:
					partType = "text-delta"
				case provider.ChunkTypeReasoning:
					partType = "reasoning-delta"
				}
			}
			metadataPart := part
			metadataPart.Type = provider.ChunkType(partType)
			return map[string]interface{}{
				"type": partType,
				"part": metadataPart,
			}
		}
		processMessageMetadata := func(part provider.StreamChunk) {
			// The start part's metadata was taken once, for the start chunk
			// sent before the source is read: TS calls messageMetadata once per
			// part, so the source's own start part does not call it again.
			if options.MessageMetadata == nil || part.Type == provider.ChunkTypeStart {
				return
			}
			input := messageMetadataInput(part, "")
			partType, _ := input["type"].(string)
			metadata := options.MessageMetadata(input)
			if metadata != nil && partType != "start" && partType != "finish" {
				processChunk(UIMessageChunk{
					"type":            "message-metadata",
					"messageMetadata": metadata,
				})
			}
		}

		var startMetadata map[string]interface{}
		if options.MessageMetadata != nil {
			startMetadata = options.MessageMetadata(messageMetadataInput(provider.StreamChunk{Type: provider.ChunkType("start")}, "start"))
		}
		if sendStart {
			startEvent := map[string]interface{}{
				"type": "start",
			}
			startMessageID := streamMessageID
			if startMessageID == "" && options.GenerateMessageID != nil {
				startMessageID = callbackMessageID
			}
			if startMessageID != "" {
				startEvent["messageId"] = startMessageID
			}
			if startMetadata != nil {
				startEvent["messageMetadata"] = startMetadata
			}
			processChunk(startEvent)
		}

		sawTerminal := false
		sawFinishChunk := false
		sawOutput := false
		// allowStepStartUI dedupes the UI "start-step" part within one step
		// boundary: a stream produced by stream.go's own StreamText loop now
		// carries BOTH the genuine, once-per-step ChunkTypeStartStep chunk
		// AND (when a provider surfaces pre-stream warnings) the older
		// ChunkTypeStreamStart chunk that used to be this package's only
		// source for the "start-step" UI part before ChunkTypeStartStep
		// existed — both map to "start-step". Only the first of either
		// within a step boundary is converted; see the dedup switch below
		// for why "finish-step" is handled differently (not deduped against
		// itself the same way).
		allowStepStartUI := true
		// sawStepFinishStep tracks whether a genuine ChunkTypeFinishStep has
		// already closed the current step boundary. stream.go's own
		// StreamText loop (and the harness bridge) always emits an explicit
		// ChunkTypeFinishStep before the call-level ChunkTypeFinish that
		// follows it, so ChunkTypeFinish must NOT also convert to its own
		// "finish-step" UI part in that case — TS's to-ui-message-chunk.ts
		// maps 'finish-step' and 'finish' to two fully independent UI part
		// types, and a 'finish' part never produces "finish-step". But a
		// bare/raw provider stream with no separate ChunkTypeFinishStep at
		// all (e.g. a hand-built StreamTextResult, or any TextStream fed
		// straight into ToUIMessageStream without going through stream.go's
		// own multi-step processStream loop) has only ChunkTypeFinish as its
		// single step-and-call boundary signal, so in that case it must
		// still convert to "finish-step" the same way a genuine
		// ChunkTypeFinishStep would (existing, pinned contract — see
		// TestStreamTextResult_ToUIMessageStream_OptionsAndCallbacks and
		// TestToUIMessageStream_MessageMetadataReceivesUIPartTypes). Reset to
		// false on every ChunkTypeStartStep/ChunkTypeStreamStart (a new step
		// boundary starting) so a multi-step external/harness stream whose
		// LAST step has no explicit ChunkTypeFinishStep of its own (only a
		// terminal ChunkTypeFinish that carries that step's own new content —
		// see stream_external.go's consumeExternalParts doc comment) still
		// gets its own "finish-step" instead of being wrongly suppressed by
		// an earlier step's ChunkTypeFinishStep (see
		// TestNewStreamTextResultFromParts_ToUIMessageStream).
		sawStepFinishStep := false
		// sawStartStepChunk marks a source that carries genuine
		// ChunkTypeStartStep parts: a StreamText or NewStreamTextResultFromParts
		// full stream, whose ChunkTypeFinish is always the TS call-level
		// 'finish' part. TS to-ui-message-chunk.ts maps 'finish' to the UI
		// "finish" part only: no "finish-step", and no closing of text or
		// reasoning blocks (a harness turn suspended mid-step ends with a
		// bare 'finish' after its open step was discarded, and leaves those
		// blocks open for the next slice). Only a raw provider stream, which
		// has no ChunkTypeStartStep, still treats ChunkTypeFinish as its
		// step boundary.
		sawStartStepChunk := false
		endedWithCallFinish := false
		// callFinishPart is the source's last ChunkTypeFinish: the TS 'finish'
		// part messageMetadata sees carries the call's finishReason,
		// rawFinishReason and totalUsage.
		var callFinishPart *provider.StreamChunk
		// lastErrChunk is the source's previous part when it was an error
		// part, else nil.
		var lastErrChunk *provider.StreamChunk
		activeTextIDs := &orderedIDSet{}
		activeReasoningIDs := &orderedIDSet{}
		textID := ""
		reasoningID := ""
		textSeq := 0
		reasoningSeq := 0
		ensureTextStart := func(id string) string {
			if id == "" {
				if textID == "" {
					textSeq++
					textID = fmt.Sprintf("text-%d", textSeq)
				}
				id = textID
			}
			if !activeTextIDs.has(id) {
				activeTextIDs.add(id)
				processChunk(UIMessageChunk{"type": "text-start", "id": id})
			}
			return id
		}
		ensureReasoningStart := func(id string) string {
			if id == "" {
				if reasoningID == "" {
					reasoningSeq++
					reasoningID = fmt.Sprintf("reasoning-%d", reasoningSeq)
				}
				id = reasoningID
			}
			if !activeReasoningIDs.has(id) {
				activeReasoningIDs.add(id)
				processChunk(UIMessageChunk{"type": "reasoning-start", "id": id})
			}
			return id
		}
		closeOpenParts := func() {
			for _, id := range activeTextIDs.snapshot() {
				processChunk(UIMessageChunk{"type": "text-end", "id": id})
				activeTextIDs.remove(id)
			}
			if sendReasoning {
				for _, id := range activeReasoningIDs.snapshot() {
					processChunk(UIMessageChunk{"type": "reasoning-end", "id": id})
					activeReasoningIDs.remove(id)
				}
			} else {
				activeReasoningIDs.clear()
			}
		}
		for {
			if processingErr != nil {
				callOnEnd()
				return
			}
			select {
			case <-ctx.Done():
				sendUIStreamErr(errCh, ctx.Err())
				callOnEnd()
				return
			default:
			}
			chunk, err := stream.Next()
			if err != nil {
				if err == io.EOF {
					break
				}
				if isAbortErr(ctx, err) {
					setSourceOutcome(UIMessageStreamOutcome{Status: UIMessageStreamOutcomeAborted})
					abortPart := provider.StreamChunk{Type: provider.ChunkTypeAbort}
					abortChunk := UIMessageChunk{"type": "abort"}
					if err.Error() != "" {
						abortPart.AbortReason = err.Error()
						abortChunk["reason"] = abortPart.AbortReason
					}
					processChunk(abortChunk)
					processMessageMetadata(abortPart)
					callOnEnd()
					return
				}
				sendUIStreamErr(errCh, err)
				// A StreamText run that TS ends with an `error` part (a model
				// call that fails to open, a failed step preparation, a model
				// stream without output) closes its Go stream with that same
				// error after the part. TS's source stream ends normally
				// there, so the part's own error chunk is the only one and
				// the outcome stays the part's failed outcome.
				if lastErrChunk == nil || lastErrChunk.Err == nil || !errors.Is(err, lastErrChunk.Err) {
					failSourceOutcome(err)
					appendErrorChunk(onError, safeEnqueue, err)
				}
				callOnEnd()
				return
			}
			lastErrChunk = nil
			if chunk.Type == provider.ChunkTypeError {
				lastErrChunk = chunk
			}

			if chunk.Type == provider.ChunkTypeFinish || chunk.Type == provider.ChunkTypeFinishStep {
				sawTerminal = true
				sawFinishChunk = true
				finishReason = chunk.FinishReason
			}
			// The source outcome only goes Completed on the call-level,
			// once-only ChunkTypeFinish — not on a per-step ChunkTypeFinishStep
			// — because setSourceOutcome's guard ignores any update once the
			// outcome is Completed, and an intermediate step's finish-step
			// must not block a later real error in the same call from being
			// recorded as Failed.
			if chunk.Type == provider.ChunkTypeFinish {
				setSourceOutcome(UIMessageStreamOutcome{Status: UIMessageStreamOutcomeCompleted})
				finishPart := *chunk
				callFinishPart = &finishPart
			}
			if chunk.Type == provider.ChunkTypeError {
				sawTerminal = true
				if finishReason == "" {
					finishReason = types.FinishReasonError
				}
				setSourceOutcome(UIMessageStreamOutcome{Status: UIMessageStreamOutcomeFailed, Error: streamChunkError(chunk)})
			}
			if isModelOutputChunkType(chunk.Type) {
				sawOutput = true
			}
			if chunk.Type == provider.ChunkTypeText && chunk.Text == "" {
				continue
			}
			if chunk.Type == provider.ChunkTypeStartStep {
				sawStartStepChunk = true
			}
			callLevelFinish := chunk.Type == provider.ChunkTypeFinish && sawStartStepChunk
			if callLevelFinish {
				endedWithCallFinish = true
			}
			chunkForConversion := *chunk
			switch chunk.Type {
			case provider.ChunkTypeTextStart:
				id := chunk.ID
				if id == "" {
					textSeq++
					id = fmt.Sprintf("text-%d", textSeq)
					textID = id
				}
				activeTextIDs.add(id)
				chunkForConversion.ID = id
			case provider.ChunkTypeText:
				chunkForConversion.ID = ensureTextStart(chunk.ID)
			case provider.ChunkTypeTextEnd:
				id := chunk.ID
				if id == "" {
					id = textID
				}
				chunkForConversion.ID = id
				activeTextIDs.remove(id)
				if id == textID {
					textID = ""
				}
			case provider.ChunkTypeReasoningStart:
				if !sendReasoning {
					break
				}
				id := chunk.ID
				if id == "" {
					reasoningSeq++
					id = fmt.Sprintf("reasoning-%d", reasoningSeq)
					reasoningID = id
				}
				activeReasoningIDs.add(id)
				chunkForConversion.ID = id
			case provider.ChunkTypeReasoning:
				if sendReasoning {
					chunkForConversion.ID = ensureReasoningStart(chunk.ID)
				}
			case provider.ChunkTypeReasoningEnd:
				if !sendReasoning {
					break
				}
				id := chunk.ID
				if id == "" {
					id = reasoningID
				}
				chunkForConversion.ID = id
				activeReasoningIDs.remove(id)
				if id == reasoningID {
					reasoningID = ""
				}
			case provider.ChunkTypeFinish, provider.ChunkTypeFinishStep:
				if !callLevelFinish {
					closeOpenParts()
				}
			}

			// Dedupe the "start-step" UI part within one step boundary (see
			// allowStepStartUI's doc comment above): a stream produced by
			// stream.go's own StreamText loop now carries the genuine,
			// once-per-step ChunkTypeStartStep chunk in addition to a
			// provider's own ChunkTypeStreamStart chunk (when it has
			// pre-stream warnings) — both map to "start-step" — so only the
			// first of either within a step boundary is converted.
			//
			// ChunkTypeFinish is skipped here ONLY when a genuine
			// ChunkTypeFinishStep already closed the current step boundary
			// (sawStepFinishStep, see its doc comment above): TS's
			// to-ui-message-chunk.ts maps 'finish-step' and 'finish' to two
			// completely independent UI part types, so once a real
			// "finish-step" has already been produced for this step, the
			// call-level ChunkTypeFinish that follows it must not ALSO
			// convert to another one — its own UI "finish" part
			// (finishReason + messageMetadata) is synthesized once below,
			// after the loop ends, from finishReason (kept up to date by
			// every ChunkTypeFinish/ChunkTypeFinishStep chunk above). See
			// TestToUIMessageStream_MultiStepFinishStepAndFinishCounts and
			// pkg/workflow/harness_test.go, whose want slices assert exactly
			// one "finish-step" per real step. ChunkTypeStreamFinish is
			// Go-only and always immediately follows a real ChunkTypeFinish,
			// so it is skipped unconditionally.
			skipStepBoundaryDup := false
			switch chunk.Type {
			case provider.ChunkTypeStartStep, provider.ChunkTypeStreamStart:
				// A new step boundary starts: reset sawStepFinishStep so a
				// later step's own terminal ChunkTypeFinish (with no
				// explicit ChunkTypeFinishStep of its own — see
				// TestNewStreamTextResultFromParts_ToUIMessageStream) is not
				// wrongly skipped because an EARLIER step's FinishStep set
				// the flag.
				sawStepFinishStep = false
				if allowStepStartUI {
					allowStepStartUI = false
				} else {
					skipStepBoundaryDup = true
				}
			case provider.ChunkTypeFinishStep:
				allowStepStartUI = true
				sawStepFinishStep = true
			case provider.ChunkTypeFinish:
				allowStepStartUI = true
				if sawStepFinishStep || callLevelFinish {
					skipStepBoundaryDup = true
				}
			case provider.ChunkTypeStreamFinish:
				skipStepBoundaryDup = true
			}
			if !skipStepBoundaryDup {
				converted := toUIMessageChunks(chunkForConversion, options)
				if chunk.Type == provider.ChunkTypeFinish || chunk.Type == provider.ChunkTypeFinishStep {
					// Apply step metadata before the finish-step boundary, so
					// the OnStepEnd snapshot includes it (TS #22268).
					processMessageMetadata(*chunk)
					for _, uiChunk := range converted {
						processChunk(uiChunk)
					}
				} else {
					for _, uiChunk := range converted {
						processChunk(uiChunk)
					}
					processMessageMetadata(*chunk)
				}
			}
		}

		if !sawTerminal && options.PartialStream {
			callOnEnd()
			return
		}
		if !sawTerminal {
			if !sawOutput {
				err := newIncompleteModelStreamError()
				setSourceOutcome(UIMessageStreamOutcome{Status: UIMessageStreamOutcomeFailed, Error: err})
				appendErrorChunk(onError, processChunk, err)
				processMessageMetadata(provider.StreamChunk{
					Type: provider.ChunkTypeError,
					Text: err.Error(),
				})
				callOnEnd()
				return
			}
			finishReason = types.FinishReasonOther
		}
		if !endedWithCallFinish {
			closeOpenParts()
		}
		if !sawFinishChunk && finishReason != "" {
			processMessageMetadata(provider.StreamChunk{
				Type:         provider.ChunkTypeFinish,
				FinishReason: finishReason,
			})
			processChunk(UIMessageChunk{"type": "finish-step"})
		}
		var finishMetadata map[string]interface{}
		if options.MessageMetadata != nil {
			finishPart := provider.StreamChunk{}
			if callFinishPart != nil {
				finishPart = *callFinishPart
			}
			finishPart.Type = provider.ChunkType("finish")
			finishPart.FinishReason = finishReason
			finishMetadata = options.MessageMetadata(messageMetadataInput(finishPart, "finish"))
		}
		if sendFinish {
			finishEvent := map[string]interface{}{
				"type":         "finish",
				"finishReason": string(finishReason),
			}
			if finishMetadata != nil {
				finishEvent["messageMetadata"] = finishMetadata
			}
			processChunk(finishEvent)
		}
		callOnEnd()
	}()
	return out, errCh
}

// ToUIMessageChunk converts a single provider stream chunk into a UI message
// chunk. The boolean return is false for stream parts that do not produce UI
// message chunks, matching TypeScript's undefined result.
func ToUIMessageChunk(part provider.StreamChunk, opts UIMessageStreamResultOptions) (UIMessageChunk, bool) {
	chunks := toUIMessageChunks(part, opts)
	if len(chunks) == 0 {
		return nil, false
	}
	return chunks[0], true
}

func toUIMessageChunks(part provider.StreamChunk, opts UIMessageStreamResultOptions) []UIMessageChunk {
	onError := getDefaultMessageErrorHandler(opts.OnError)
	sendReasoning := boolOption(opts.SendReasoning, true)
	sendSources := boolOption(opts.SendSources, false)
	switch part.Type {
	case provider.ChunkTypeStreamStart:
		return []UIMessageChunk{{"type": "start-step"}}
	case provider.ChunkTypeFinish, provider.ChunkTypeFinishStep:
		// A bare/raw provider stream (no separate ChunkTypeFinishStep, e.g. a
		// hand-built StreamTextResult or a TextStream fed straight into
		// ToUIMessageStream without going through stream.go's own multi-step
		// processStream loop) has only ChunkTypeFinish as its single
		// step-and-call boundary signal, so it must still map to "finish-step"
		// here (see TestStreamTextResult_ToUIMessageStream_OptionsAndCallbacks,
		// TestToUIMessageStream_MessageMetadataReceivesUIPartTypes). When a
		// real per-step ChunkTypeFinishStep already closed the step, the main
		// loop's dedup switch skips calling this function for the subsequent
		// call-level ChunkTypeFinish entirely (see sawStepFinishStep below),
		// so this case is never reached redundantly for that chunk.
		return []UIMessageChunk{{"type": "finish-step"}}
	case provider.ChunkTypeAbort:
		chunk := UIMessageChunk{"type": "abort"}
		if part.AbortReason != "" {
			chunk["reason"] = part.AbortReason
		}
		return []UIMessageChunk{chunk}
	}

	chunks := convertProviderChunkToUIMessageChunks(part, resultChunkConversionOptions{
		SendReasoning: sendReasoning,
		SendSources:   sendSources,
		OnError:       onError,
		Tools:         opts.Tools,
	})
	return chunks
}

func responseUIMessageID(options UIMessageStreamResultOptions) string {
	if options.ResponseMessageID != "" {
		return options.ResponseMessageID
	}
	if options.OriginalMessages == nil {
		return ""
	}
	if len(options.OriginalMessages) > 0 {
		last := options.OriginalMessages[len(options.OriginalMessages)-1]
		if role, _ := last["role"].(string); role == "assistant" {
			id, _ := last["id"].(string)
			return id
		}
	}
	if options.GenerateMessageID != nil {
		return options.GenerateMessageID()
	}
	return ""
}

type uiMessageCallbackState struct {
	originalMessages []UIMessageChunk
	message          UIMessageChunk
	isContinuation   bool
	// lastMessageID is the id of the continued assistant message.
	lastMessageID   string
	isAborted       bool
	finishReason    interface{}
	activeText      map[string]UIMessageChunk
	activeReasoning map[string]UIMessageChunk
	partialTools    map[string]*uiPartialToolCall

	// resetOnMessageIDChange discards the seeded message (parts, metadata and
	// in-flight part state) when a start chunk carries a different message
	// ID, so a resumed stream for another response does not adopt content
	// from an unrelated assistant turn. Mirrors TS processUIMessageStream's
	// resetStateOnMessageIdChange.
	resetOnMessageIDChange bool
}

type uiPartialToolCall struct {
	text         string
	toolName     string
	dynamic      bool
	title        interface{}
	toolMetadata interface{}
	// inputEncoding is "text" when the input deltas are raw text rather than
	// partial JSON (TS partialToolCalls[].inputEncoding).
	inputEncoding string
}

func newUIMessageCallbackState(original []UIMessageChunk, messageID string) *uiMessageCallbackState {
	state := &uiMessageCallbackState{
		originalMessages: append([]UIMessageChunk(nil), original...),
		message: UIMessageChunk{
			"id":    messageID,
			"role":  "assistant",
			"parts": []interface{}{},
		},
		activeText:      map[string]UIMessageChunk{},
		activeReasoning: map[string]UIMessageChunk{},
		partialTools:    map[string]*uiPartialToolCall{},
	}
	if len(original) > 0 {
		last := original[len(original)-1]
		if role, _ := last["role"].(string); role == "assistant" {
			state.isContinuation = true
			state.lastMessageID = stringValue(last["id"])
			state.message = cloneUIMessageChunk(last)
			if _, ok := state.message["parts"]; !ok {
				state.message["parts"] = []interface{}{}
			}
			state.partialTools = seedPartialToolCalls(state.message)
		}
	}
	return state
}

// seedPartialToolCalls reconstructs the partial-tool-call accumulator for
// any tool part in message's last step that is still in the
// "input-streaming" state, keyed by toolCallId. This lets a resumed stream
// continue a "tool-input-delta" sequence for a tool call that was already
// partially streamed before a disconnect (the part's "rawInput" carries the
// accumulated raw text) -- the part itself was hydrated from persisted
// history, so no "tool-input-start" chunk precedes the resumed deltas.
// Mirrors TS process-ui-message-stream.ts's createStreamingUIMessageState
// (audit: TS #21480).
func seedPartialToolCalls(message UIMessageChunk) map[string]*uiPartialToolCall {
	partials := map[string]*uiPartialToolCall{}
	parts := uiParts(message)
	start := currentStepStartIndex(parts)
	for _, raw := range parts[start:] {
		part := asUIPartChunk(raw)
		if !isToolPartChunk(part) || stringValue(part["state"]) != "input-streaming" {
			continue
		}
		toolCallID := stringValue(part["toolCallId"])
		if toolCallID == "" {
			continue
		}
		toolName, dynamic := toolInfoFromPart(part)
		partials[toolCallID] = &uiPartialToolCall{
			text:          stringValue(part["rawInput"]),
			toolName:      toolName,
			dynamic:       dynamic,
			title:         part["title"],
			toolMetadata:  part["toolMetadata"],
			inputEncoding: stringValue(part["inputEncoding"]),
		}
	}
	return partials
}

func (s *uiMessageCallbackState) responseMessage() UIMessageChunk {
	return cloneUIMessageChunk(s.message)
}

// continuation reports whether the response continues the last original
// assistant message: TS `state.message.id === lastMessage?.id`, so a start
// chunk that gives the response another id ends the continuation.
func (s *uiMessageCallbackState) continuation() bool {
	return s.isContinuation && stringValue(s.message["id"]) == s.lastMessageID
}

func (s *uiMessageCallbackState) messages() []UIMessageChunk {
	messages := append([]UIMessageChunk(nil), s.originalMessages...)
	if s.continuation() && len(messages) > 0 {
		messages = messages[:len(messages)-1]
	}
	messages = append(messages, s.responseMessage())
	return messages
}

func (s *uiMessageCallbackState) apply(chunk UIMessageChunk, onError func(error) string) {
	chunkType, _ := chunk["type"].(string)
	switch chunkType {
	case "start":
		// TS `chunk.messageId != null`: an empty id is still applied.
		if id, ok := chunk["messageId"].(string); ok {
			if s.resetOnMessageIDChange && id != stringValue(s.message["id"]) {
				s.message = UIMessageChunk{"id": id, "role": "assistant", "parts": []interface{}{}}
				s.activeText = map[string]UIMessageChunk{}
				s.activeReasoning = map[string]UIMessageChunk{}
				s.partialTools = map[string]*uiPartialToolCall{}
				s.finishReason = nil
				s.isContinuation = false
			}
			s.message["id"] = id
		}
		s.mergeMetadata(chunk["messageMetadata"])
	case "finish":
		if reason := chunk["finishReason"]; reason != nil {
			s.finishReason = reason
		}
		s.mergeMetadata(chunk["messageMetadata"])
	case "message-metadata":
		s.mergeMetadata(chunk["messageMetadata"])
	case "abort":
		s.isAborted = true
	case "text-start":
		id, _ := chunk["id"].(string)
		part := UIMessageChunk{"type": "text", "text": "", "state": "streaming"}
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.activeText[id] = part
		s.appendPart(part)
	case "text-delta":
		id, _ := chunk["id"].(string)
		part := s.activeText[id]
		if part == nil {
			reportMissingUIMessagePart(onError, "text-delta", id)
			return
		}
		part["text"] = stringValue(part["text"]) + stringValue(chunk["delta"])
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
	case "text-end":
		id, _ := chunk["id"].(string)
		part := s.activeText[id]
		if part == nil {
			reportMissingUIMessagePart(onError, "text-end", id)
			return
		}
		part["state"] = "done"
		copyIfPresent(part, chunk, "citations", "citations")
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		delete(s.activeText, id)
	case "reasoning-start":
		id, _ := chunk["id"].(string)
		part := UIMessageChunk{"type": "reasoning", "text": "", "state": "streaming"}
		// TS `id: chunk.id`: an empty id is kept.
		if _, ok := chunk["id"].(string); ok {
			part["id"] = id
		}
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.activeReasoning[id] = part
		s.appendPart(part)
	case "reasoning-delta":
		id, _ := chunk["id"].(string)
		part := s.activeReasoning[id]
		if part == nil {
			reportMissingUIMessagePart(onError, "reasoning-delta", id)
			return
		}
		part["text"] = stringValue(part["text"]) + stringValue(chunk["delta"])
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
	case "reasoning-end":
		id, _ := chunk["id"].(string)
		part := s.activeReasoning[id]
		if part == nil {
			reportMissingUIMessagePart(onError, "reasoning-end", id)
			return
		}
		part["state"] = "done"
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		delete(s.activeReasoning, id)
	case "file", "reasoning-file":
		part := UIMessageChunk{"type": chunkType}
		copyIfPresent(part, chunk, "mediaType", "mediaType")
		copyIfPresent(part, chunk, "url", "url")
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.appendPart(part)
	case "source-url":
		part := UIMessageChunk{"type": "source-url"}
		copyIfPresent(part, chunk, "sourceId", "sourceId")
		copyIfPresent(part, chunk, "url", "url")
		copyIfPresent(part, chunk, "title", "title")
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.appendPart(part)
	case "source-document":
		part := UIMessageChunk{"type": "source-document"}
		copyIfPresent(part, chunk, "sourceId", "sourceId")
		copyIfPresent(part, chunk, "mediaType", "mediaType")
		copyIfPresent(part, chunk, "title", "title")
		copyIfPresent(part, chunk, "filename", "filename")
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.appendPart(part)
	case "custom":
		part := UIMessageChunk{"type": "custom"}
		copyIfPresent(part, chunk, "kind", "kind")
		copyIfPresent(part, chunk, "providerMetadata", "providerMetadata")
		s.appendPart(part)
	case "start-step":
		s.appendPart(UIMessageChunk{"type": "step-start"})
	case "finish-step":
		// Active parts are closed by their explicit end chunks, not by
		// finish-step: a merged stream's step can finish while another
		// stream's text/reasoning part is still active. Mirrors TS
		// process-ui-message-stream.ts's now-empty 'finish-step' case.
	case "reset-step":
		// A model-call step is being retried (e.g. WorkflowAgent). Drop the
		// parts added since the last step-start and clear in-flight state so
		// the retried attempt starts clean. Mirrors TS 'reset-step'.
		start := currentStepStartIndex(uiParts(s.message))
		s.activeText = map[string]UIMessageChunk{}
		s.activeReasoning = map[string]UIMessageChunk{}
		s.partialTools = map[string]*uiPartialToolCall{}
		truncateUIParts(s.message, start)
	case "tool-input-start":
		toolCallID := stringValue(chunk["toolCallId"])
		toolName := stringValue(chunk["toolName"])
		dynamic, _ := chunk["dynamic"].(bool)
		s.partialTools[toolCallID] = &uiPartialToolCall{
			toolName:      toolName,
			dynamic:       dynamic,
			title:         chunk["title"],
			toolMetadata:  chunk["toolMetadata"],
			inputEncoding: stringValue(chunk["inputEncoding"]),
		}
		startUpdate := UIMessageChunk{
			"state":    "input-streaming",
			"input":    uiOmitField,
			"rawInput": uiOmitField,
		}
		if enc := stringValue(chunk["inputEncoding"]); enc != "" {
			startUpdate["inputEncoding"] = enc
		}
		s.updateToolPart(toolCallID, toolName, dynamic, startUpdate, chunk)
	case "tool-input-delta":
		toolCallID := stringValue(chunk["toolCallId"])
		partial := s.partialTools[toolCallID]
		if partial == nil {
			reportMissingToolInput(onError, "tool-input-delta", toolCallID)
			return
		}
		partial.text += stringValue(chunk["inputTextDelta"])
		// rawInput carries the accumulated raw text onto the part itself (not
		// just the in-memory partialTools accumulator) so a persisted/hydrated
		// message can later re-seed partialTools via seedPartialToolCalls and
		// continue streaming after a disconnect. Mirrors TS
		// process-ui-message-stream.ts's tool-input-delta handling (TS #21480).
		// Text-encoded deltas (e.g. OpenAI custom tools) accumulate raw text;
		// everything else is partial JSON (TS inputEncoding === 'text').
		var partialInput interface{}
		if partial.inputEncoding == "text" {
			partialInput = partial.text
		} else {
			partialInput = parseToolInputPartial(partial.text)
		}
		deltaUpdate := UIMessageChunk{
			"state":    "input-streaming",
			"input":    partialInput,
			"rawInput": partial.text,
		}
		if partial.inputEncoding != "" {
			deltaUpdate["inputEncoding"] = partial.inputEncoding
		}
		s.updateToolPart(toolCallID, partial.toolName, partial.dynamic, deltaUpdate, UIMessageChunk{"title": partial.title, "toolMetadata": partial.toolMetadata})
	case "tool-input-available":
		toolCallID := stringValue(chunk["toolCallId"])
		toolName := stringValue(chunk["toolName"])
		dynamic, _ := chunk["dynamic"].(bool)
		s.updateToolPart(toolCallID, toolName, dynamic, UIMessageChunk{
			"state":    "input-available",
			"input":    uiFieldOrOmit(chunk, "input"),
			"rawInput": uiOmitField,
		}, chunk)
	case "tool-input-error":
		toolCallID := stringValue(chunk["toolCallId"])
		toolName := stringValue(chunk["toolName"])
		dynamic, _ := chunk["dynamic"].(bool)
		// When a part already exists for this toolCallId in the current step,
		// honour its type so it is updated in place.
		if existing := s.findCurrentStepToolPart(toolCallID, nil); existing != nil {
			dynamic = existing["type"] == "dynamic-tool"
		}
		// Both dynamic and static tool parts store the failed input in
		// "input" directly, without the deprecated "rawInput" compatibility
		// field: an SDK-generated output-error part must not retain a
		// deprecated field or warn about one. Persisted streams that already
		// carry a legacy rawInput continue to go through the separate
		// validate-ui-messages compatibility path.
		update := UIMessageChunk{"state": "output-error", "errorText": chunk["errorText"], "input": uiFieldOrOmit(chunk, "input")}
		// TS passes no title for tool-input-error (the chunk's title is not
		// applied): a new part has none, an existing part keeps its own.
		s.updateToolPart(toolCallID, toolName, dynamic, update, withoutTitle(chunk))
	case "tool-output-available":
		toolCallID := stringValue(chunk["toolCallId"])
		part := s.findToolPart(toolCallID)
		if part == nil {
			reportMissingToolInvocation(onError, toolCallID)
			return
		}
		toolName, dynamic := toolInfoFromPart(part)
		s.updateExistingToolPart(part, toolName, dynamic, UIMessageChunk{
			"state":       "output-available",
			"input":       uiFieldOrOmit(part, "input"),
			"output":      chunk["output"],
			"preliminary": chunk["preliminary"],
		}, withoutTitle(chunk))
	case "tool-output-error":
		toolCallID := stringValue(chunk["toolCallId"])
		part := s.findToolPart(toolCallID)
		if part == nil {
			reportMissingToolInvocation(onError, toolCallID)
			return
		}
		toolName, dynamic := toolInfoFromPart(part)
		update := UIMessageChunk{
			"state":     "output-error",
			"input":     uiFieldOrOmit(part, "input"),
			"errorText": chunk["errorText"],
		}
		// TS keeps rawInput for static tool parts only.
		if !dynamic {
			update["rawInput"] = part["rawInput"]
		}
		s.updateExistingToolPart(part, toolName, dynamic, update, withoutTitle(chunk))
	case "tool-output-denied":
		toolCallID := stringValue(chunk["toolCallId"])
		if part := s.findToolPart(toolCallID); part != nil {
			part["state"] = "output-denied"
		} else {
			reportMissingToolInvocation(onError, toolCallID)
		}
	case "tool-approval-request":
		toolCallID := stringValue(chunk["toolCallId"])
		if part := s.findToolPart(toolCallID); part != nil {
			part["state"] = "approval-requested"
			approval := UIMessageChunk{"id": chunk["approvalId"]}
			copyIfPresent(approval, chunk, "approvalDescriptor", "descriptor")
			if inputSchemaInput, ok := chunk["inputSchemaInput"]; ok {
				approval["inputSchemaInput"] = inputSchemaInput
			}
			copyIfPresent(approval, chunk, "reason", "requestReason")
			if auto, ok := chunk["isAutomatic"].(bool); ok && auto {
				approval["isAutomatic"] = true
			}
			copyIfPresent(approval, chunk, "signature", "signature")
			part["approval"] = approval
		} else {
			reportMissingToolInvocation(onError, toolCallID)
		}
	case "tool-approval-response":
		approvalID := stringValue(chunk["approvalId"])
		if part := s.findToolPartByApprovalID(approvalID); part != nil {
			part["state"] = "approval-responded"
			// Start from the previous approval so descriptor, requestReason,
			// isAutomatic, signature and inputSchemaInput survive the
			// approval-responded transition (also when the approval was
			// restored from a persisted message).
			approval := UIMessageChunk{}
			if previous, ok := asUIMap(part["approval"]); ok {
				for key, value := range previous {
					approval[key] = value
				}
			}
			approval["id"] = chunk["approvalId"]
			approval["approved"] = chunk["approved"]
			copyIfPresent(approval, chunk, "reason", "reason")
			part["approval"] = approval
			copyIfPresent(part, chunk, "providerExecuted", "providerExecuted")
			copyIfPresent(part, chunk, "providerMetadata", "callProviderMetadata")
		} else {
			reportMissingApproval(onError, approvalID)
		}
	case "error":
		if onError != nil {
			_ = onError(errors.New(stringValue(chunk["errorText"])))
		}
	default:
		if strings.HasPrefix(chunkType, "data-") {
			if transient, _ := chunk["transient"].(bool); transient {
				return
			}
			s.upsertDataPart(chunk)
		}
	}
}

func (s *uiMessageCallbackState) mergeMetadata(metadata interface{}) {
	if metadata == nil {
		return
	}
	if existing, ok := asUIMap(s.message["metadata"]); ok {
		if incoming, ok := asUIMap(metadata); ok {
			s.message["metadata"] = mergeUIMaps(existing, incoming)
			return
		}
	}
	s.message["metadata"] = cloneUIValue(metadata)
}

func (s *uiMessageCallbackState) appendPart(part UIMessageChunk) {
	if typedParts, ok := s.message["parts"].([]UIMessageChunk); ok {
		parts := make([]interface{}, 0, intsafe.AddCap(len(typedParts), 1))
		for _, typedPart := range typedParts {
			parts = append(parts, typedPart)
		}
		s.message["parts"] = append(parts, part)
		return
	}
	parts, _ := s.message["parts"].([]interface{})
	s.message["parts"] = append(parts, part)
}

func (s *uiMessageCallbackState) upsertDataPart(chunk UIMessageChunk) {
	// TS `dataChunk.id != null`: an empty id still names a part.
	id, hasID := chunk["id"].(string)
	if hasID {
		for _, raw := range uiParts(s.message) {
			part, ok := raw.(UIMessageChunk)
			if !ok {
				if m, mapOK := raw.(map[string]interface{}); mapOK {
					part = UIMessageChunk(m)
				}
			}
			if part != nil && part["type"] == chunk["type"] && part["id"] == id {
				part["data"] = cloneUIValue(chunk["data"])
				return
			}
		}
	}
	s.appendPart(cloneUIMessageChunk(chunk))
}

// updateToolPart updates the tool part of the same kind (static or dynamic)
// with toolCallID in the current step, or appends a new part. Tool call IDs
// can repeat across steps, so earlier steps are never updated here.
func (s *uiMessageCallbackState) updateToolPart(toolCallID, toolName string, dynamic bool, update UIMessageChunk, source UIMessageChunk) {
	part := s.findCurrentStepToolPart(toolCallID, &dynamic)
	s.applyToolPartUpdate(part, toolCallID, toolName, dynamic, update, source)
}

// updateExistingToolPart updates a tool invocation located by findToolPart.
func (s *uiMessageCallbackState) updateExistingToolPart(part UIMessageChunk, toolName string, dynamic bool, update UIMessageChunk, source UIMessageChunk) {
	s.applyToolPartUpdate(part, stringValue(part["toolCallId"]), toolName, dynamic, update, source)
}

// uiOmitField is a sentinel for applyToolPartUpdate: an update value equal
// to uiOmitField deletes the key from the part instead of setting it to
// Go nil (which would marshal as JSON null). This distinguishes TS's
// `field: undefined` (key entirely absent from the JSON-serialized part,
// e.g. input-streaming/output-error tool parts' "input") from a
// genuinely-null field value, which must round-trip as JSON null. Mirrors
// TS process-ui-message-stream.ts's updateToolPart({input: undefined, ...})
// calls (audit row #2852a84).
var uiOmitField = struct{}{}

func (s *uiMessageCallbackState) applyToolPartUpdate(part UIMessageChunk, toolCallID, toolName string, dynamic bool, update UIMessageChunk, source UIMessageChunk) {
	if part == nil {
		if dynamic {
			part = UIMessageChunk{"type": "dynamic-tool", "toolName": toolName, "toolCallId": toolCallID}
		} else {
			part = UIMessageChunk{"type": "tool-" + toolName, "toolCallId": toolCallID}
		}
		s.appendPart(part)
	}
	if toolName != "" && part["type"] == "dynamic-tool" {
		part["toolName"] = toolName
	} else if toolName != "" && !dynamic {
		// A static part adopts the completed tool name (TS #23583): when
		// repairToolCall renamed the tool after input started streaming, the
		// part type must follow so convertToModelMessages rebuilds the
		// repaired name and signed approvals keep verifying.
		part["type"] = "tool-" + toolName
	}
	// TS updateToolPart/updateDynamicToolPart assign output, errorText, rawInput
	// and preliminary on every update, so a field the update does not carry is
	// cleared rather than left over from the previous state.
	for _, key := range []string{"output", "errorText", "rawInput", "preliminary", "inputEncoding"} {
		delete(part, key)
	}
	for key, value := range update {
		if value == uiOmitField {
			delete(part, key)
			continue
		}
		// A nil rawInput/preliminary is TS undefined (absent), not JSON null.
		if value == nil && (key == "rawInput" || key == "preliminary") {
			continue
		}
		part[key] = value
	}
	copyIfPresent(part, source, "providerExecuted", "providerExecuted")
	copyIfPresent(part, source, "title", "title")
	copyIfPresent(part, source, "toolMetadata", "toolMetadata")
	if _, isOutput := update["output"]; isOutput {
		copyIfPresent(part, source, "providerMetadata", "resultProviderMetadata")
	} else if update["state"] == "output-error" {
		copyIfPresent(part, source, "providerMetadata", "resultProviderMetadata")
	} else {
		copyIfPresent(part, source, "providerMetadata", "callProviderMetadata")
	}
}

// uiFieldOrOmit is m[key], or uiOmitField when m has no such key: TS copies
// an undefined input as undefined (absent), not null.
func uiFieldOrOmit(m UIMessageChunk, key string) interface{} {
	if value, ok := m[key]; ok {
		return value
	}
	return uiOmitField
}

// withoutTitle returns chunk without its title. TS passes no title for
// tool-input-error and the invocation's own title for tool-output-available
// and tool-output-error, so the chunk's title is never applied for them.
func withoutTitle(chunk UIMessageChunk) UIMessageChunk {
	source := make(UIMessageChunk, len(chunk))
	for k, v := range chunk {
		if k != "title" {
			source[k] = v
		}
	}
	return source
}

// applyUIStateChunk applies chunk to the callback state and returns the
// processing failure TS processUIMessageStream throws (a delta, end or tool
// chunk with no matching part), which errors the stream. An `error` chunk is
// reported through onError and is not a failure.
func applyUIStateChunk(state *uiMessageCallbackState, chunk UIMessageChunk, onError func(error) string) (failure error) {
	// An onError that panics for an error chunk fails the processing too
	// (TS: the error thrown in the transform errors the stream).
	defer func() {
		if r := recover(); r != nil {
			failure = panicToError(r)
		}
	}()
	isErrorChunk := chunk["type"] == "error"
	state.apply(chunk, func(err error) string {
		if isErrorChunk {
			return onError(err)
		}
		if failure == nil {
			failure = err
		}
		return ""
	})
	return failure
}

// findToolPart returns the tool invocation for toolCallID, preferring the
// current step and falling back to the latest matching part in the message
// (TS getToolInvocation).
func (s *uiMessageCallbackState) findToolPart(toolCallID string) UIMessageChunk {
	if part := s.findCurrentStepToolPart(toolCallID, nil); part != nil {
		return part
	}
	parts := uiParts(s.message)
	for i := len(parts) - 1; i >= 0; i-- {
		part := asUIPartChunk(parts[i])
		if isToolPartChunk(part) && part["toolCallId"] == toolCallID {
			return part
		}
	}
	return nil
}

// currentStepStartIndex returns the index of the first part after the last
// step-start part in parts (0 if there is no step-start). Mirrors TS
// process-ui-message-stream.ts's getCurrentStepParts boundary search.
func currentStepStartIndex(parts []interface{}) int {
	i := len(parts) - 1
	for i >= 0 {
		if part := asUIPartChunk(parts[i]); part != nil && part["type"] == "step-start" {
			break
		}
		i--
	}
	return i + 1
}

// truncateUIParts drops every part at or after start from message's parts
// slice, preserving its concrete slice type ([]UIMessageChunk vs
// []interface{}).
func truncateUIParts(message UIMessageChunk, start int) {
	if typedParts, ok := message["parts"].([]UIMessageChunk); ok {
		if start < len(typedParts) {
			message["parts"] = typedParts[:start]
		}
		return
	}
	if parts, ok := message["parts"].([]interface{}); ok {
		if start < len(parts) {
			message["parts"] = parts[:start]
		}
	}
}

// findCurrentStepToolPart finds a tool part with toolCallID after the last
// step-start part. When dynamic is non-nil, only parts of that kind match.
func (s *uiMessageCallbackState) findCurrentStepToolPart(toolCallID string, dynamic *bool) UIMessageChunk {
	parts := uiParts(s.message)
	start := currentStepStartIndex(parts)
	for _, raw := range parts[start:] {
		part := asUIPartChunk(raw)
		if !isToolPartChunk(part) || part["toolCallId"] != toolCallID {
			continue
		}
		if dynamic != nil && (part["type"] == "dynamic-tool") != *dynamic {
			continue
		}
		return part
	}
	return nil
}

func asUIPartChunk(raw interface{}) UIMessageChunk {
	switch part := raw.(type) {
	case UIMessageChunk:
		return part
	case map[string]interface{}:
		return UIMessageChunk(part)
	}
	return nil
}

func isToolPartChunk(part UIMessageChunk) bool {
	if part == nil {
		return false
	}
	partType := stringValue(part["type"])
	return partType == "dynamic-tool" || strings.HasPrefix(partType, "tool-")
}

func (s *uiMessageCallbackState) findToolPartByApprovalID(approvalID string) UIMessageChunk {
	for _, raw := range uiParts(s.message) {
		part, ok := raw.(UIMessageChunk)
		if !ok {
			if m, ok := raw.(map[string]interface{}); ok {
				part = UIMessageChunk(m)
			}
		}
		if part == nil {
			continue
		}
		approval, _ := part["approval"].(UIMessageChunk)
		if approval == nil {
			if m, ok := part["approval"].(map[string]interface{}); ok {
				approval = UIMessageChunk(m)
			}
		}
		if approval != nil && approval["id"] == approvalID {
			return part
		}
	}
	return nil
}

func toolInfoFromPart(part UIMessageChunk) (string, bool) {
	if part == nil {
		return "", false
	}
	if part["type"] == "dynamic-tool" {
		return stringValue(part["toolName"]), true
	}
	typeName := stringValue(part["type"])
	return strings.TrimPrefix(typeName, "tool-"), false
}

func uiParts(message UIMessageChunk) []interface{} {
	if parts, ok := message["parts"].([]interface{}); ok {
		return parts
	}
	if typedParts, ok := message["parts"].([]UIMessageChunk); ok {
		parts := make([]interface{}, 0, len(typedParts))
		for _, part := range typedParts {
			parts = append(parts, part)
		}
		return parts
	}
	return nil
}

func copyIfPresent(dst UIMessageChunk, src UIMessageChunk, from, to string) {
	if value, ok := src[from]; ok && value != nil {
		dst[to] = value
	}
}

func stringValue(value interface{}) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// parseToolInputPartial is TS parsePartialJson(text).value: incomplete JSON
// is repaired, so a streaming tool part shows the input received so far.
// Text that cannot be parsed or repaired gives TS undefined, which
// applyToolPartUpdate treats as no input key (uiOmitField).
func parseToolInputPartial(text string) interface{} {
	result := jsonparser.ParsePartialJSON(text)
	if result.State == jsonparser.ParseStateFailed {
		return uiOmitField
	}
	return result.Value
}

// UIMessageStreamError is the error a UI message stream fails with when it
// contains invalid or out-of-sequence chunks: a delta/end chunk received
// without a corresponding start chunk, or a tool invocation/approval
// response that can't be matched to a toolCallId/approvalId. As in TS, the
// stream checks this only while it builds the message for an OnEnd or
// OnStepEnd callback; ReadUIMessages reports it through OnError. Mirrors TS's
// AI_UIMessageStreamError (error/ui-message-stream-error.ts).
type UIMessageStreamError struct {
	// ChunkType is the type of chunk that caused the error (e.g. "text-delta", "reasoning-end").
	ChunkType string

	// ChunkID is the ID associated with the failing chunk (part ID or toolCallId/approvalId).
	ChunkID string

	// Message describes the error.
	Message string
}

func (e *UIMessageStreamError) Error() string {
	return e.Message
}

// IsUIMessageStreamError reports whether err is a *UIMessageStreamError.
func IsUIMessageStreamError(err error) bool {
	var target *UIMessageStreamError
	return errors.As(err, &target)
}

// The report functions build TS's template-literal messages: the id is quoted
// as is, not escaped.
func reportMissingUIMessagePart(onError func(error) string, chunkType, chunkID string) {
	if onError == nil {
		return
	}
	partType := "text"
	if strings.HasPrefix(chunkType, "reasoning-") {
		partType = "reasoning"
	}
	startType := partType + "-start"
	_ = onError(&UIMessageStreamError{
		ChunkType: chunkType,
		ChunkID:   chunkID,
		Message:   fmt.Sprintf(`Received %s for missing %s part with ID "%s". Ensure a "%s" chunk is sent before any "%s" chunks.`, chunkType, partType, chunkID, startType, chunkType),
	})
}

func reportMissingToolInput(onError func(error) string, chunkType, toolCallID string) {
	if onError == nil {
		return
	}
	_ = onError(&UIMessageStreamError{
		ChunkType: chunkType,
		ChunkID:   toolCallID,
		Message:   fmt.Sprintf(`Received %s for missing tool call with ID "%s". Ensure a "tool-input-start" chunk is sent before any "%s" chunks.`, chunkType, toolCallID, chunkType),
	})
}

func reportMissingToolInvocation(onError func(error) string, toolCallID string) {
	if onError == nil {
		return
	}
	_ = onError(&UIMessageStreamError{
		ChunkType: "tool-invocation",
		ChunkID:   toolCallID,
		Message:   fmt.Sprintf(`No tool invocation found for tool call ID "%s".`, toolCallID),
	})
}

func reportMissingApproval(onError func(error) string, approvalID string) {
	if onError == nil {
		return
	}
	_ = onError(&UIMessageStreamError{
		ChunkType: "tool-approval-response",
		ChunkID:   approvalID,
		Message:   fmt.Sprintf(`No tool invocation found for approval ID "%s".`, approvalID),
	})
}

func cloneUIMessageChunk(in UIMessageChunk) UIMessageChunk {
	if in == nil {
		return nil
	}
	out := make(UIMessageChunk, len(in))
	for key, value := range in {
		out[key] = cloneUIValue(value)
	}
	return out
}

func cloneUIValue(value interface{}) interface{} {
	switch v := value.(type) {
	case UIMessageChunk:
		return cloneUIMessageChunk(v)
	case map[string]interface{}:
		return cloneUIMessageChunk(UIMessageChunk(v))
	case []UIMessageChunk:
		out := make([]UIMessageChunk, len(v))
		for i, item := range v {
			out[i] = cloneUIMessageChunk(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = cloneUIValue(item)
		}
		return out
	default:
		return v
	}
}

func asUIMap(value interface{}) (map[string]interface{}, bool) {
	switch v := value.(type) {
	case UIMessageChunk:
		return map[string]interface{}(v), true
	case map[string]interface{}:
		return v, true
	default:
		return nil, false
	}
}

func mergeUIMaps(base, incoming map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, intsafe.AddCap(len(base), len(incoming)))
	for key, value := range base {
		out[key] = cloneUIValue(value)
	}
	for key, value := range incoming {
		if key == "__proto__" || key == "constructor" || key == "prototype" {
			continue
		}
		if existing, ok := asUIMap(out[key]); ok {
			if next, ok := asUIMap(value); ok {
				out[key] = mergeUIMaps(existing, next)
				continue
			}
		}
		out[key] = cloneUIValue(value)
	}
	return out
}

// CreateUIMessageStreamResponse writes UI chunks as SSE to an HTTP response.
func CreateUIMessageStreamResponse(ctx context.Context, result *StreamTextResult, opts ...UIMessageStreamResultOptions) (*http.Response, error) {
	return CreateUIMessageStreamResponseWithInit(ctx, result, nil, opts...)
}

// UIMessageStreamHeaders returns the headers a UI message stream response
// carries: SSE content type, no caching or proxy buffering, and the
// X-Vercel-AI-UI-Message-Stream protocol version that useChat expects. The
// Pipe* helpers set them on an http.ResponseWriter for you. Mirrors TS
// UI_MESSAGE_STREAM_HEADERS.
func UIMessageStreamHeaders() http.Header {
	// Set canonicalizes the keys; a map literal with "X-Vercel-AI-UI-..."
	// would be invisible to Header.Get.
	h := http.Header{}
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Vercel-AI-UI-Message-Stream", "v1")
	h.Set("X-Accel-Buffering", "no")
	return h
}

// validateUIMessageStreamKeepAlive rejects a KeepAliveMs that is not a
// positive timer duration of at most 2147483647ms, as TS
// createSseStreamWithKeepAlive throws before the response starts.
func validateUIMessageStreamKeepAlive(init *UIMessageStreamResponseInit) error {
	if init == nil || init.KeepAliveMs == nil {
		return nil
	}
	if d := *init.KeepAliveMs; d <= 0 || d > 2147483647*time.Millisecond {
		return errors.New("keepAliveMs must be a positive finite timer duration no greater than 2147483647")
	}
	return nil
}

// uiMessageStreamResponseHead resolves the status and headers for a UI
// message stream response from the defaults and init.
func uiMessageStreamResponseHead(init *UIMessageStreamResponseInit) (int, string, http.Header) {
	status := http.StatusOK
	statusText := ""
	headers := UIMessageStreamHeaders()
	if init != nil {
		if init.Status != 0 {
			status = init.Status
		}
		statusText = init.StatusText
		for key, value := range init.Headers {
			headers.Set(key, value)
		}
		for key, values := range init.Header {
			for _, value := range values {
				headers.Add(key, value)
			}
		}
	}
	return status, statusText, headers
}

// CreateUIMessageStreamResponseWithInit writes UI chunks as SSE to an HTTP response
// with optional status/statusText/headers.
func CreateUIMessageStreamResponseWithInit(ctx context.Context, result *StreamTextResult, init *UIMessageStreamResponseInit, opts ...UIMessageStreamResultOptions) (*http.Response, error) {
	if result == nil {
		return nil, fmt.Errorf("result is required")
	}
	if err := validateUIMessageStreamKeepAlive(init); err != nil {
		return nil, err
	}

	status, statusText, headers := uiMessageStreamResponseHead(init)

	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = pw.Close() }()
		_ = PipeUIMessageStreamToResponseWithInit(ctx, result, pw, init, opts...)
	}()
	return &http.Response{
		StatusCode: status,
		Status:     statusText,
		Header:     headers,
		Body:       pr,
	}, nil
}

// PipeUIMessageStreamToResponse writes UI chunks to the given writer as SSE.
// When w is an http.ResponseWriter it first sets UIMessageStreamHeaders and
// writes status 200, like TS pipeUIMessageStreamToResponse.
func PipeUIMessageStreamToResponse(ctx context.Context, result *StreamTextResult, w io.Writer, opts ...UIMessageStreamResultOptions) error {
	return PipeUIMessageStreamToResponseWithInit(ctx, result, w, nil, opts...)
}

// writeUIMessageSSEChunk JSON-encodes and writes a single UI message chunk
// as one SSE "data: ...\n\n" event, then flushes it.
func writeUIMessageSSEChunk(bw *bufio.Writer, flush func() error, chunk UIMessageChunk) error {
	b, err := jsonstr.Marshal(chunk)
	if err != nil {
		return err
	}
	if _, err := bw.WriteString("data: "); err != nil {
		return err
	}
	if _, err := bw.Write(b); err != nil {
		return err
	}
	if _, err := bw.WriteString("\n\n"); err != nil {
		return err
	}
	return flush()
}

// PipeUIMessageStreamToResponseWithInit supports optional SSE side-channel consumption.
//
// If writing to w fails (for example because the client disconnected), the
// internal source stream is cancelled before returning so its background
// producer goroutine and any upstream provider resources it holds are
// released instead of leaking (blocked forever trying to send a chunk to a
// reader that has stopped listening). Mirrors TS
// write-to-server-response.ts's client-disconnect handling, which cancels
// the ReadableStream's reader on a premature close (TS #21578).
func PipeUIMessageStreamToResponseWithInit(ctx context.Context, result *StreamTextResult, w io.Writer, init *UIMessageStreamResponseInit, opts ...UIMessageStreamResultOptions) error {
	if result == nil {
		return fmt.Errorf("result is required")
	}
	if w == nil {
		return fmt.Errorf("writer is required")
	}
	if err := validateUIMessageStreamKeepAlive(init); err != nil {
		return err
	}

	// streamCtx is cancelled on any write/flush failure (the deferred cancel
	// runs as soon as writing stops) so the CreateUIMessageStream producer
	// goroutine stops promptly instead of relying solely on the caller's ctx,
	// which may never be cancelled.
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	chunks, errCh := CreateUIMessageStream(streamCtx, result, opts...)
	status, _, headers := uiMessageStreamResponseHead(init)
	writeResponseHead(w, status, headers)
	consumeErr, err := writeUIMessageChunksSSE(chunks, w, init)
	if err != nil {
		return err
	}
	select {
	case err := <-errCh:
		if consumeErr != nil {
			return fmt.Errorf("%v; consumeSSEStream error: %w", err, consumeErr)
		}
		return err
	default:
		return consumeErr
	}
}

// PipeUIMessageChunksToResponse writes any stream of UI message chunks to w
// as Server-Sent Events, ending with "data: [DONE]". Use it for streams that
// don't come from a single StreamTextResult, such as the output of
// CreateUIMessageStreamWithOptions or CreateAgentUIStreamFromUIMessages.
// Mirrors TS pipeUIMessageStreamToResponse({ response, stream }).
//
// When w is an http.ResponseWriter it first sets the UI message stream
// headers (UIMessageStreamHeaders, overridden by init's) and writes the
// status (200 unless init sets one). Other writers get only the body. init
// may be nil.
//
// If writing fails, PipeUIMessageChunksToResponse returns without draining
// chunks. Create the stream with a ctx you cancel when this returns (an
// http.Request's context is cancelled when the client disconnects) so its
// producer stops instead of blocking.
func PipeUIMessageChunksToResponse(chunks <-chan UIMessageChunk, w io.Writer, init *UIMessageStreamResponseInit) error {
	if chunks == nil {
		return fmt.Errorf("stream is required")
	}
	if w == nil {
		return fmt.Errorf("writer is required")
	}
	if err := validateUIMessageStreamKeepAlive(init); err != nil {
		return err
	}
	status, _, headers := uiMessageStreamResponseHead(init)
	writeResponseHead(w, status, headers)
	consumeErr, err := writeUIMessageChunksSSE(chunks, w, init)
	if err != nil {
		return err
	}
	return consumeErr
}

// CreateUIMessageChunksResponse returns an *http.Response that streams any
// UI message chunk stream as Server-Sent Events, with the same status and
// headers as CreateUIMessageStreamResponse. Mirrors TS
// createUIMessageStreamResponse({ stream }).
func CreateUIMessageChunksResponse(chunks <-chan UIMessageChunk, init *UIMessageStreamResponseInit) (*http.Response, error) {
	if chunks == nil {
		return nil, fmt.Errorf("stream is required")
	}
	if err := validateUIMessageStreamKeepAlive(init); err != nil {
		return nil, err
	}
	status, statusText, headers := uiMessageStreamResponseHead(init)
	pr, pw := io.Pipe()
	go func() {
		err := PipeUIMessageChunksToResponse(chunks, pw, init)
		_ = pw.CloseWithError(err)
	}()
	return &http.Response{
		StatusCode: status,
		Status:     statusText,
		Header:     headers,
		Body:       pr,
	}, nil
}

// writeUIMessageChunksSSE is the shared SSE writer behind the Pipe*
// helpers. It returns the ConsumeSSEStream callback's error separately from
// a write error.
func writeUIMessageChunksSSE(chunks <-chan UIMessageChunk, w io.Writer, init *UIMessageStreamResponseInit) (consumeErr error, err error) {
	var (
		teeWriter  = w
		sideWriter *io.PipeWriter
		closeSide  chan error
	)
	if init != nil && init.ConsumeSSEStream != nil {
		pr, pw := io.Pipe()
		sideWriter = pw
		closeSide = make(chan error, 1)
		go func() {
			closeSide <- init.ConsumeSSEStream(pr)
			// A consumer that stops reading early must not block the
			// response writes the tee feeds into the side pipe (TS tees
			// the stream, so each branch reads at its own pace).
			_, _ = io.Copy(io.Discard, pr)
		}()
		defer func() { _ = pw.Close() }()
		teeWriter = io.MultiWriter(w, pw)
	}

	var keepAliveDuration time.Duration
	keepAliveEnabled := false
	if init != nil && init.KeepAliveMs != nil {
		keepAliveDuration = *init.KeepAliveMs
		keepAliveEnabled = true
	}

	bw := bufio.NewWriter(teeWriter)
	// flush surfaces the bufio flush error instead of discarding it (TS
	// write-to-server-response.ts's pipe helpers return a promise that
	// rejects on a write/flush failure), and additionally calls the
	// underlying writer's Flush when it implements http.Flusher so an SSE
	// consumer receives each event immediately instead of in ~4KiB batches
	// (audit rows #56/#76).
	flusher, _ := w.(http.Flusher)
	flush := func() error {
		if err := bw.Flush(); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	writeComment := func(comment string) error {
		if _, err := bw.WriteString(comment); err != nil {
			return err
		}
		return flush()
	}

	if keepAliveEnabled {
		// Flush an opening comment immediately, before the first real chunk
		// is available, so a reverse proxy sees the response start right
		// away instead of timing out an idle connection. Mirrors TS
		// createSseStreamWithKeepAlive's STREAM_OPEN_COMMENT.
		if err := writeComment(sseStreamOpenComment); err != nil {
			return nil, err
		}
	}

	if !keepAliveEnabled {
		for chunk := range chunks {
			if err := writeUIMessageSSEChunk(bw, flush, chunk); err != nil {
				return nil, err
			}
		}
	} else {
		timer := time.NewTimer(keepAliveDuration)
		defer timer.Stop()
	loop:
		for {
			select {
			case chunk, ok := <-chunks:
				if !ok {
					break loop
				}
				if err := writeUIMessageSSEChunk(bw, flush, chunk); err != nil {
					return nil, err
				}
				timer.Reset(keepAliveDuration)
			case <-timer.C:
				if err := writeComment(sseKeepAliveComment); err != nil {
					return nil, err
				}
				timer.Reset(keepAliveDuration)
			}
		}
	}
	if _, err := bw.WriteString("data: [DONE]\n\n"); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if sideWriter != nil {
		_ = sideWriter.Close()
		consumeErr = <-closeSide
	}
	return consumeErr, nil
}

// ReadUIMessageStream reads SSE data lines containing JSON UI chunks.
func ReadUIMessageStream(r io.Reader) ([]UIMessageChunk, error) {
	if r == nil {
		return nil, fmt.Errorf("reader is required")
	}
	sc := bufio.NewScanner(r)
	// UI message chunks can carry inline file/image data well over
	// bufio.Scanner's default 64 KiB token limit on a single `data: ` line.
	// Match the bound used for the shared SSEParser (R4-4).
	sc.Buffer(make([]byte, 64*1024), 32*1024*1024)
	chunks := make([]UIMessageChunk, 0)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			continue
		}
		var chunk UIMessageChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

type resultChunkConversionOptions struct {
	SendReasoning bool
	SendSources   bool
	OnError       func(error) string
	Tools         []types.Tool
}

func convertProviderChunkToUIMessageChunks(chunk provider.StreamChunk, opts resultChunkConversionOptions) []UIMessageChunk {
	out := make([]UIMessageChunk, 0, 2)

	withMeta := func(part map[string]interface{}) {
		if len(chunk.ProviderMetadata) == 0 {
			return
		}
		var providerMetadata map[string]interface{}
		if err := json.Unmarshal(chunk.ProviderMetadata, &providerMetadata); err == nil && len(providerMetadata) > 0 {
			part["providerMetadata"] = providerMetadata
		}
	}
	// Mirrors TS toUIMessageChunk isDynamic: a registered tool decides on its
	// own type (so an invalid call to a static tool stays a static UI part);
	// only tools missing from the tool set (provider-executed dynamic tools,
	// NoSuchTool calls) fall back to the part's own dynamic flag.
	isDynamicTool := func(toolName string, dynamic bool) bool {
		for _, tool := range opts.Tools {
			if tool.Name == toolName {
				return tool.Type == types.ToolTypeDynamic
			}
		}
		return dynamic
	}

	switch chunk.Type {
	case provider.ChunkTypeRaw:
		break
	case provider.ChunkTypeResponseMetadata, provider.ChunkTypeFirstChunk:
		break
	case provider.ChunkTypeTextStart:
		part := map[string]interface{}{"type": "text-start", "id": chunk.ID}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeText:
		part := map[string]interface{}{"type": "text-delta", "id": chunk.ID, "delta": chunk.Text}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeTextEnd:
		part := map[string]interface{}{"type": "text-end", "id": chunk.ID}
		if len(chunk.Citations) > 0 {
			// Round-trip through JSON so the chunk holds the same generic
			// shape as a decoded wire chunk.
			if raw, err := json.Marshal(chunk.Citations); err == nil {
				var citations []interface{}
				if json.Unmarshal(raw, &citations) == nil {
					part["citations"] = citations
				}
			}
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeReasoningStart:
		if !opts.SendReasoning {
			break
		}
		part := map[string]interface{}{"type": "reasoning-start", "id": chunk.ID}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeReasoningEnd:
		if !opts.SendReasoning {
			break
		}
		part := map[string]interface{}{"type": "reasoning-end", "id": chunk.ID}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeReasoning:
		if !opts.SendReasoning {
			break
		}
		delta := chunk.Reasoning
		if delta == "" {
			delta = chunk.Text
		}
		part := map[string]interface{}{"type": "reasoning-delta", "id": chunk.ID, "delta": delta}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeSource:
		if chunk.SourceContent == nil || !opts.SendSources {
			break
		}
		// TS part.providerMetadata is the source's own metadata; providers
		// keep it on SourceContent (a chunk-level copy wins).
		if len(chunk.ProviderMetadata) == 0 {
			chunk.ProviderMetadata = chunk.SourceContent.ProviderMetadata
		}
		if chunk.SourceContent.SourceType == "url" {
			part := map[string]interface{}{
				"type":     "source-url",
				"sourceId": chunk.SourceContent.ID,
				"url":      chunk.SourceContent.URL,
			}
			if chunk.SourceContent.Title != "" {
				part["title"] = chunk.SourceContent.Title
			}
			withMeta(part)
			out = append(out, part)
			break
		}
		if chunk.SourceContent.SourceType == "document" {
			// A document source's title is a required string (TS writes
			// it even when empty; the UI message schema requires it).
			part := map[string]interface{}{
				"type":      "source-document",
				"sourceId":  chunk.SourceContent.ID,
				"mediaType": chunk.SourceContent.MediaType,
				"title":     chunk.SourceContent.Title,
			}
			if chunk.SourceContent.Filename != "" {
				part["filename"] = chunk.SourceContent.Filename
			}
			withMeta(part)
			out = append(out, part)
		}
	case provider.ChunkTypeFile:
		fileContent := chunk.GeneratedFileContent
		if fileContent == nil {
			break
		}
		part := map[string]interface{}{
			"type":      "file",
			"mediaType": fileContent.MediaType,
			"url":       deriveFileURL(fileContent.MediaType, fileContent),
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeReasoningFile:
		if !opts.SendReasoning {
			break
		}
		fileContent := chunk.ReasoningFileContent
		if fileContent == nil {
			break
		}
		part := map[string]interface{}{
			"type":      "reasoning-file",
			"mediaType": fileContent.MediaType,
			"url":       deriveFileURLFromReasoning(fileContent, fileContent.MediaType),
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeCustom:
		if chunk.CustomContent == nil {
			break
		}
		part := map[string]interface{}{
			"type": "custom",
			"kind": chunk.CustomContent.Kind,
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeToolCall:
		if chunk.ToolCall == nil {
			break
		}
		if chunk.ToolCall.Invalid {
			part := map[string]interface{}{
				"type":       "tool-input-error",
				"toolCallId": chunk.ToolCall.ID,
				"toolName":   chunk.ToolCall.ToolName,
				"input":      invalidToolCallInput(*chunk.ToolCall),
				"errorText":  opts.OnError(chunk.ToolCall.Error),
			}
			if chunk.ToolCall.ProviderExecuted || chunk.ToolCall.ProviderExecutedSet {
				part["providerExecuted"] = chunk.ToolCall.ProviderExecuted
			}
			if len(chunk.ToolCall.ToolMetadata) > 0 {
				part["toolMetadata"] = chunk.ToolCall.ToolMetadata
			}
			if isDynamicTool(chunk.ToolCall.ToolName, chunk.ToolCall.Dynamic) {
				part["dynamic"] = true
			}
			if chunk.ToolCall.Title != "" {
				part["title"] = chunk.ToolCall.Title
			}
			if len(chunk.ToolCall.ProviderMetadata) > 0 {
				part["providerMetadata"] = chunk.ToolCall.ProviderMetadata
			}
			withMeta(part)
			out = append(out, part)
			break
		}
		part := map[string]interface{}{
			"type":       "tool-input-available",
			"toolCallId": chunk.ToolCall.ID,
			"toolName":   chunk.ToolCall.ToolName,
			"input":      chunk.ToolCall.Arguments,
		}
		// TS forwards providerExecuted whenever it is not undefined, so an
		// explicit false (ProviderExecutedSet) is kept.
		if chunk.ToolCall.ProviderExecuted || chunk.ToolCall.ProviderExecutedSet {
			part["providerExecuted"] = chunk.ToolCall.ProviderExecuted
		}
		if len(chunk.ToolCall.ToolMetadata) > 0 {
			part["toolMetadata"] = chunk.ToolCall.ToolMetadata
		}
		if isDynamicTool(chunk.ToolCall.ToolName, chunk.ToolCall.Dynamic) {
			part["dynamic"] = true
		}
		if chunk.ToolCall.Title != "" {
			part["title"] = chunk.ToolCall.Title
		}
		if len(chunk.ToolCall.ProviderMetadata) > 0 {
			part["providerMetadata"] = chunk.ToolCall.ProviderMetadata
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeToolInputStart:
		if chunk.ToolCall == nil {
			break
		}
		part := map[string]interface{}{
			"type":       "tool-input-start",
			"toolCallId": chunk.ToolCall.ID,
			"toolName":   chunk.ToolCall.ToolName,
		}
		if chunk.ToolCall.InputEncoding != "" {
			part["inputEncoding"] = chunk.ToolCall.InputEncoding
		}
		// TS forwards providerExecuted whenever it is not undefined, so an
		// explicit false (ProviderExecutedSet) is kept.
		if chunk.ToolCall.ProviderExecuted || chunk.ToolCall.ProviderExecutedSet {
			part["providerExecuted"] = chunk.ToolCall.ProviderExecuted
		}
		if len(chunk.ToolCall.ToolMetadata) > 0 {
			part["toolMetadata"] = chunk.ToolCall.ToolMetadata
		}
		if isDynamicTool(chunk.ToolCall.ToolName, chunk.ToolCall.Dynamic) {
			part["dynamic"] = true
		}
		if chunk.ToolCall.Title != "" {
			part["title"] = chunk.ToolCall.Title
		}
		if len(chunk.ToolCall.ProviderMetadata) > 0 {
			part["providerMetadata"] = chunk.ToolCall.ProviderMetadata
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeToolInputDelta:
		part := map[string]interface{}{
			"type":           "tool-input-delta",
			"toolCallId":     chunk.ID,
			"inputTextDelta": chunk.Text,
		}
		out = append(out, part)
	case provider.ChunkTypeToolInputEnd:
		break
	case provider.ChunkTypeToolApprovalRequest:
		if chunk.ToolApprovalRequest == nil {
			break
		}
		part := map[string]interface{}{
			"type":       "tool-approval-request",
			"approvalId": chunk.ToolApprovalRequest.ApprovalID,
			"toolCallId": chunk.ToolApprovalRequest.ToolCallID,
		}
		if part["toolCallId"] == "" {
			part["toolCallId"] = chunk.ToolApprovalRequest.ToolCall.ID
		}
		if chunk.ToolApprovalRequest.IsAutomatic {
			part["isAutomatic"] = true
		}
		if chunk.ToolApprovalRequest.Signature != "" {
			part["signature"] = chunk.ToolApprovalRequest.Signature
		}
		if chunk.ToolApprovalRequest.InputSchemaInput != nil {
			part["inputSchemaInput"] = chunk.ToolApprovalRequest.InputSchemaInput
		}
		if chunk.ToolApprovalRequest.Reason != "" {
			part["reason"] = chunk.ToolApprovalRequest.Reason
		}
		out = append(out, part)
	case provider.ChunkTypeToolApprovalResponse:
		if chunk.ToolApprovalResponse == nil {
			break
		}
		part := map[string]interface{}{
			"type":       "tool-approval-response",
			"approvalId": chunk.ToolApprovalResponse.ApprovalID,
			"approved":   chunk.ToolApprovalResponse.Approved,
		}
		if chunk.ToolApprovalResponse.Reason != "" {
			part["reason"] = chunk.ToolApprovalResponse.Reason
		}
		// TS forwards providerExecuted when it is not undefined, so an
		// explicit false (ProviderExecutedSet) is kept.
		if chunk.ToolApprovalResponse.ProviderExecuted || chunk.ToolApprovalResponse.ProviderExecutedSet {
			part["providerExecuted"] = chunk.ToolApprovalResponse.ProviderExecuted
		}
		out = append(out, part)
	case provider.ChunkTypeToolOutputDenied:
		toolCallID := ""
		if chunk.ToolResult != nil {
			toolCallID = chunk.ToolResult.ToolCallID
		}
		out = append(out, map[string]interface{}{
			"type":       "tool-output-denied",
			"toolCallId": toolCallID,
		})
	case provider.ChunkTypeToolResult:
		if chunk.ToolResult == nil {
			break
		}
		approvalID := chunk.ToolResult.ApprovalID
		if approvalID == "" {
			approvalID = chunk.ToolResult.ToolCallID
		}
		if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusUserApproval ||
			chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusApproved ||
			chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusDenied {
			request := map[string]interface{}{
				"type":       "tool-approval-request",
				"approvalId": approvalID,
				"toolCallId": chunk.ToolResult.ToolCallID,
			}
			if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusApproved ||
				chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusDenied {
				request["isAutomatic"] = true
			}
			if chunk.ToolResult.ApprovalSignature != "" {
				request["signature"] = chunk.ToolResult.ApprovalSignature
			}
			if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusUserApproval && chunk.ToolResult.ApprovalReason != nil {
				request["reason"] = *chunk.ToolResult.ApprovalReason
			}
			out = append(out, request)
			if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusUserApproval {
				break
			}
			response := map[string]interface{}{
				"type":       "tool-approval-response",
				"approvalId": approvalID,
				"approved":   chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusApproved,
			}
			if chunk.ToolResult.ApprovalReason != nil {
				response["reason"] = *chunk.ToolResult.ApprovalReason
			}
			if chunk.ToolResult.ProviderExecuted || chunk.ToolResult.ProviderExecutedSet {
				response["providerExecuted"] = chunk.ToolResult.ProviderExecuted
			}
			out = append(out, response)
			if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusDenied {
				out = append(out, map[string]interface{}{
					"type":       "tool-output-denied",
					"toolCallId": chunk.ToolResult.ToolCallID,
				})
				break
			}
			if chunk.ToolResult.ProviderExecuted && chunk.ToolResult.Result == nil && chunk.ToolResult.Error == nil {
				break
			}
		}
		if chunk.ToolResult.ApprovalStatus == types.ToolApprovalStatusDenied {
			part := map[string]interface{}{
				"type":       "tool-output-denied",
				"toolCallId": chunk.ToolResult.ToolCallID,
			}
			out = append(out, part)
			break
		}
		partType := "tool-output-available"
		part := map[string]interface{}{
			"type":       partType,
			"toolCallId": chunk.ToolResult.ToolCallID,
			"output":     chunk.ToolResult.Result,
		}
		if len(chunk.ToolResult.ToolMetadata) > 0 {
			part["toolMetadata"] = chunk.ToolResult.ToolMetadata
		}
		if chunk.ToolResult.Error != nil {
			errorText := opts.OnError(chunk.ToolResult.Error)
			if chunk.ToolResult.ProviderExecuted {
				// TS: typeof error === 'string' ? error : JSON.stringify(error).
				// A structured provider error travels in Result.
				errorText = chunk.ToolResult.Error.Error()
				switch v := chunk.ToolResult.Result.(type) {
				case nil:
				case string:
					errorText = v
				default:
					if b, err := jsonstr.Marshal(v); err == nil {
						errorText = string(b)
					}
				}
			}
			partType = "tool-output-error"
			part = map[string]interface{}{
				"type":       partType,
				"toolCallId": chunk.ToolResult.ToolCallID,
				"errorText":  errorText,
			}
			if len(chunk.ToolResult.ToolMetadata) > 0 {
				part["toolMetadata"] = chunk.ToolResult.ToolMetadata
			}
		}
		// TS forwards providerExecuted when it is not undefined. An approved
		// result's flag came from its tool call (for the approval response);
		// the locally executed output itself has none in TS.
		if chunk.ToolResult.ProviderExecuted {
			part["providerExecuted"] = true
		} else if chunk.ToolResult.ProviderExecutedSet && chunk.ToolResult.ApprovalStatus != types.ToolApprovalStatusApproved {
			part["providerExecuted"] = false
		}
		if isDynamicTool(chunk.ToolResult.ToolName, chunk.ToolResult.Dynamic) {
			part["dynamic"] = true
		}
		if chunk.ToolResult.Preliminary {
			part["preliminary"] = true
		}
		if len(chunk.ToolResult.ProviderMetadata) > 0 {
			part["providerMetadata"] = chunk.ToolResult.ProviderMetadata
		}
		withMeta(part)
		out = append(out, part)
	case provider.ChunkTypeError:
		out = append(out, map[string]interface{}{
			"type":      "error",
			"errorText": opts.OnError(streamChunkError(&chunk)),
		})
	// ChunkTypeFinish is handled by toUIMessageChunks' own switch (alongside
	// ChunkTypeFinishStep) before this function is ever reached, so it is
	// intentionally absent here.
	default:
		if chunk.Type == "start-step" {
			out = append(out, map[string]interface{}{"type": "start-step"})
			break
		}
		if chunk.Type == provider.ChunkTypeStreamStart {
			out = append(out, map[string]interface{}{"type": "start-step"})
			break
		}
		if chunk.Type == provider.ChunkTypeStreamFinish {
			out = append(out, map[string]interface{}{"type": "finish-step"})
			break
		}
	}
	return out
}

// deriveFileURL is the UI file part url: TS `data:${mediaType};base64,${file.base64}`.
// StreamText downloads URL-backed files first, so they carry bytes; a file
// that still holds only a URL (a raw provider stream passed straight to
// ToUIMessageStream) keeps that URL rather than embedding it in a data URL.
func deriveFileURL(mediaType string, file *types.GeneratedFileContent) string {
	if file == nil {
		return ""
	}
	if data, ok := inlineFileBase64(file.FileData, file.Data); ok {
		return "data:" + mediaType + ";base64," + data
	}
	if file.FileData.Type == types.FileDataTypeURL && file.FileData.URL != "" {
		return file.FileData.URL
	}
	if file.URL != "" {
		return file.URL
	}
	return "data:" + mediaType + ";base64,"
}

// inlineFileBase64 returns the base64 of a file's inline bytes, if any.
func inlineFileBase64(fileData types.FileData, data []byte) (string, bool) {
	if fileData.Type == types.FileDataTypeData {
		if fileData.DataString != "" {
			return fileData.DataString, true
		}
		if len(fileData.Data) > 0 {
			return base64.StdEncoding.EncodeToString(fileData.Data), true
		}
	}
	if len(data) > 0 {
		return base64.StdEncoding.EncodeToString(data), true
	}
	return "", false
}

func deriveFileURLFromReasoning(file *types.ReasoningFileContent, mediaType string) string {
	if file == nil {
		return ""
	}
	if data, ok := inlineFileBase64(file.FileData, file.Data); ok {
		return "data:" + mediaType + ";base64," + data
	}
	if file.FileData.Type == types.FileDataTypeURL && file.FileData.URL != "" {
		return file.FileData.URL
	}
	return "data:" + mediaType + ";base64,"
}

// streamChunkError returns the error carried by an error chunk: chunk.Err when
// the provider set it (keeping its type for errors.As in onError handlers),
// else an error built from chunk.Text.
func streamChunkError(chunk *provider.StreamChunk) error {
	if chunk.Err != nil {
		return chunk.Err
	}
	return errors.New(chunk.Text)
}

// invalidToolCallInput is the input of an invalid tool call as TS reports it
// in a tool-input-error chunk: the parsed value when the model's input text
// parsed, the text itself otherwise (parseToolCall's catch block).
func invalidToolCallInput(call types.ToolCall) interface{} {
	if call.Arguments != nil {
		return call.Arguments
	}
	if call.RawArguments != "" {
		return call.RawArguments
	}
	return call.Arguments
}

// orderedIDSet is a set of open block ids that remembers insertion order, so
// the text-end/reasoning-end parts synthesized when a step closes come out in
// the order the blocks were opened instead of in random map order.
type orderedIDSet struct {
	order []string
	set   map[string]bool
}

func (s *orderedIDSet) has(id string) bool { return s.set[id] }

func (s *orderedIDSet) add(id string) {
	if s.set == nil {
		s.set = map[string]bool{}
	}
	if !s.set[id] {
		s.set[id] = true
		s.order = append(s.order, id)
	}
}

func (s *orderedIDSet) remove(id string) {
	if !s.set[id] {
		return
	}
	delete(s.set, id)
	for i, v := range s.order {
		if v == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

func (s *orderedIDSet) snapshot() []string { return append([]string(nil), s.order...) }

func (s *orderedIDSet) clear() { s.order, s.set = nil, nil }
