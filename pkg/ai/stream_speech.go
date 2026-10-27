package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// StreamSpeechOptions configures ExperimentalStreamSpeech.
type StreamSpeechOptions struct {
	// Model must implement provider.SpeechStreamer.
	Model provider.SpeechModel

	// Text is the complete text to convert to speech. Set either Text or
	// TextStream, not both.
	Text string

	// TextStream is a stream of text deltas or text parts (for example the
	// text of a language model stream) that is synthesized as it arrives. A
	// text part can set its own Voice on providers that support several
	// voices per stream. The SDK owns the stream once the call is made and
	// cancels it on failure and cancellation. See NewChannelSpeechTextStream.
	TextStream provider.SpeechTextStream

	// Voice to use for speech generation.
	Voice string

	// OutputFormat is the desired format of the output audio chunks, e.g.
	// {Type: "audio/pcm", Rate: &rate}. Supported formats are
	// provider-specific.
	OutputFormat *provider.AudioFormat

	// Instructions for the speech generation.
	Instructions string

	// Speed of the speech generation.
	Speed *float64

	// Language is an ISO 639-1 code (e.g. "en") or "auto". Provider support
	// varies.
	Language string

	// ProviderOptions contains additional provider-specific options.
	ProviderOptions map[string]interface{}

	// Headers are additional headers for the request, when supported by the
	// provider.
	Headers map[string]string

	// IncludeRawChunks includes raw provider chunks in FullStream.
	IncludeRawChunks bool

	// OnError is called for each error part the provider reports. Error parts
	// are not fatal: FullStream includes them and AudioStream skips them. It
	// runs on its own goroutine, and a panic in it is ignored. Defaults to
	// printing the error to standard error.
	OnError func(err error)
}

// SpeechStreamPart is one part of ExperimentalStreamSpeech's FullStream (TS
// SpeechStreamPart). stream-start, response-metadata and finish parts from
// the provider are consumed internally and surface via the result accessors.
type SpeechStreamPart struct {
	// Type is "audio", "raw" or "error".
	Type string

	// ID is an optional provider identifier for an audio chunk.
	ID string

	// Audio is an audio chunk in generation order. Chunks are not
	// necessarily standalone files; concatenate them to reconstruct the
	// audio. A chunk is empty only when it carries ProviderMetadata (for
	// example a turn marker).
	Audio            []byte
	AudioFormat      provider.AudioFormat
	ProviderMetadata map[string]interface{}

	// RawValue carries the provider's raw chunk when Type is "raw".
	RawValue interface{}

	// Err carries the error payload when Type is "error".
	Err interface{}
}

// SpeechStreamUsage is usage information reported for a speech stream call.
type SpeechStreamUsage = provider.SpeechStreamUsage

// SpeechStream is a Next()-based stream of SpeechStreamPart, following the
// same convention as provider.TextStream.
type SpeechStream interface {
	Next() (*SpeechStreamPart, error)
	Err() error
	Close() error
}

// SpeechAudioStream is a Next()-based stream of audio bytes in generation
// order. Next returns io.EOF when the stream is complete.
type SpeechAudioStream interface {
	Next() ([]byte, error)
	Err() error
	Close() error
}

var errSpeechStreamCancelled = errors.New("Speech stream was cancelled.") //nolint:staticcheck // matches TS SDK's exact error text

// StreamSpeechResult is the result of ExperimentalStreamSpeech.
//
// AudioStream and FullStream share one live stream: only one of them can be
// accessed, and only once. Access it before any result accessor: calling an
// accessor first consumes the stream internally. The stream is never
// buffered, so the call makes progress only while the output is consumed or
// until the context is cancelled or a stream is closed.
type StreamSpeechResult struct {
	usageP            *streamPromise[SpeechStreamUsage]
	warningsP         *streamPromise[[]types.Warning]
	responsesP        *streamPromise[[]SpeechModelResponseMetadata]
	providerMetadataP *streamPromise[map[string]interface{}]

	mu          sync.Mutex
	streamOwner string // "unclaimed" | "audio-stream" | "full-stream" | "result-promises"
	ch          chan SpeechStreamPart
	ctx         context.Context
	pipeDone    chan struct{}
	cancel      context.CancelCauseFunc

	finalErrMu sync.Mutex
	finalErr   error
}

func (r *StreamSpeechResult) setFinalErr(err error) {
	r.finalErrMu.Lock()
	defer r.finalErrMu.Unlock()
	if r.finalErr == nil {
		r.finalErr = err
	}
}

func (r *StreamSpeechResult) getFinalErr() error {
	r.finalErrMu.Lock()
	defer r.finalErrMu.Unlock()
	return r.finalErr
}

func (r *StreamSpeechResult) rejectAll(err error) {
	r.usageP.reject(err)
	r.warningsP.reject(err)
	r.responsesP.reject(err)
	r.providerMetadataP.reject(err)
}

func (r *StreamSpeechResult) consumeStream() {
	r.mu.Lock()
	if r.streamOwner != "unclaimed" {
		r.mu.Unlock()
		return
	}
	r.streamOwner = "result-promises"
	r.mu.Unlock()
	go func() {
		for range r.ch { //nolint:revive // drain; results surface via the accessors
		}
	}()
}

// Usage returns usage information reported for the call, blocking until the
// stream finishes.
func (r *StreamSpeechResult) Usage() (SpeechStreamUsage, error) {
	r.consumeStream()
	return r.usageP.wait()
}

// Warnings returns warnings for the call, e.g. unsupported settings.
func (r *StreamSpeechResult) Warnings() ([]types.Warning, error) {
	r.consumeStream()
	return r.warningsP.wait()
}

// Responses returns response metadata.
func (r *StreamSpeechResult) Responses() ([]SpeechModelResponseMetadata, error) {
	r.consumeStream()
	return r.responsesP.wait()
}

// ProviderMetadata returns additional provider-specific metadata.
func (r *StreamSpeechResult) ProviderMetadata() (map[string]interface{}, error) {
	r.consumeStream()
	return r.providerMetadataP.wait()
}

func (r *StreamSpeechResult) claim(owner string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.streamOwner != "unclaimed" {
		if r.streamOwner == "result-promises" {
			name := "audioStream"
			if owner == "full-stream" {
				name = "fullStream"
			}
			return fmt.Errorf("%s cannot be accessed after a result promise.", name) //nolint:staticcheck // matches TS SDK's exact error text
		}
		return errors.New("Only one of audioStream or fullStream can be accessed, and only once.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	r.streamOwner = owner
	return nil
}

// FullStream returns the full stream of speech parts (audio, raw and error
// parts). See StreamSpeechResult for the single-consumer rules.
func (r *StreamSpeechResult) FullStream() (SpeechStream, error) {
	if err := r.claim("full-stream"); err != nil {
		return nil, err
	}
	return &speechFullStream{result: r}, nil
}

// AudioStream returns the stream of audio bytes in generation order. Error
// parts are non-fatal and skipped; only stream failures end the audio with an
// error. See StreamSpeechResult for the single-consumer rules.
func (r *StreamSpeechResult) AudioStream() (SpeechAudioStream, error) {
	if err := r.claim("audio-stream"); err != nil {
		return nil, err
	}
	return &speechAudioStream{parts: &speechFullStream{result: r}}, nil
}

type speechFullStream struct {
	result    *StreamSpeechResult
	err       error
	closeOnce sync.Once
}

func (s *speechFullStream) Next() (*SpeechStreamPart, error) {
	r := s.result
	select {
	case part, ok := <-r.ch:
		return s.deliver(part, ok)
	case <-r.ctx.Done():
		// A finished pipe cancels its context after closing ch, so only
		// treat the cancellation as an error while the pipe is still live.
		select {
		case <-r.pipeDone:
			part, ok := <-r.ch
			return s.deliver(part, ok)
		default:
		}
		err := r.getFinalErr()
		if err == nil {
			err = context.Cause(r.ctx)
		}
		s.err = err
		return nil, err
	}
}

func (s *speechFullStream) deliver(part SpeechStreamPart, ok bool) (*SpeechStreamPart, error) {
	if !ok {
		if err := s.result.getFinalErr(); err != nil {
			s.err = err
			return nil, err
		}
		return nil, io.EOF
	}
	return &part, nil
}

func (s *speechFullStream) Err() error { return s.err }

func (s *speechFullStream) Close() error {
	s.closeOnce.Do(func() { s.result.cancel(errSpeechStreamCancelled) })
	return nil
}

type speechAudioStream struct {
	parts *speechFullStream
	err   error
}

func (s *speechAudioStream) Next() ([]byte, error) {
	for {
		part, err := s.parts.Next()
		if err != nil {
			if err != io.EOF {
				s.err = err
			}
			return nil, err
		}
		if part.Type == provider.SpeechStreamPartTypeAudio && len(part.Audio) > 0 {
			return part.Audio, nil
		}
	}
}

func (s *speechAudioStream) Err() error { return s.err }

func (s *speechAudioStream) Close() error { return s.parts.Close() }

// ExperimentalStreamSpeech streams speech audio using a speech model that
// implements provider.SpeechStreamer. The text is a complete string or a
// stream of text deltas and parts that is synthesized as it arrives.
//
// Cancelling ctx or closing FullStream/AudioStream aborts a pending or
// in-flight provider stream and rejects the pending result accessors. The
// model owns the text stream once the call is made and cancels it on every
// failure path.
func ExperimentalStreamSpeech(ctx context.Context, opts StreamSpeechOptions) (*StreamSpeechResult, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if opts.TextStream != nil && opts.Text != "" {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "text",
			Message: "Set either Text or TextStream, not both.",
		}
	}
	streamer, ok := opts.Model.(provider.SpeechStreamer)
	if !ok {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "streaming speech",
			Message: fmt.Sprintf(
				"The %s model %q does not support streaming speech.",
				opts.Model.Provider(), opts.Model.ModelID(),
			),
		}
	}
	text := opts.TextStream
	if text == nil {
		text = &singleSpeechTextStream{part: provider.SpeechStreamTextPart{Text: opts.Text}}
	}

	pipeCtx, cancel := context.WithCancelCause(ctx)
	result := &StreamSpeechResult{
		usageP:            newStreamPromise[SpeechStreamUsage](),
		warningsP:         newStreamPromise[[]types.Warning](),
		responsesP:        newStreamPromise[[]SpeechModelResponseMetadata](),
		providerMetadataP: newStreamPromise[map[string]interface{}](),
		streamOwner:       "unclaimed",
		ch:                make(chan SpeechStreamPart),
		ctx:               pipeCtx,
		pipeDone:          make(chan struct{}),
		cancel:            cancel,
	}

	onError := opts.OnError
	if onError == nil {
		onError = func(err error) { fmt.Fprintln(os.Stderr, err) }
	}

	go runSpeechStreamPipe(pipeCtx, streamer, opts, text, onError, result)
	return result, nil
}

// speechErrorFromPayload converts an error part's payload to an error.
func speechErrorFromPayload(payload interface{}) error {
	switch v := payload.(type) {
	case error:
		return v
	case string:
		return errors.New(v)
	default:
		return fmt.Errorf("%v", v)
	}
}

func runSpeechStreamPipe(pipeCtx context.Context, streamer provider.SpeechStreamer, opts StreamSpeechOptions, text provider.SpeechTextStream, onError func(error), result *StreamSpeechResult) {
	// Order matters: the output channel closes before pipeDone, and the
	// context is cancelled last, so a consumer that sees the cancellation can
	// tell a finished pipe from an aborted one.
	defer result.cancel(nil)
	defer close(result.pipeDone)
	defer close(result.ch)

	modelID := opts.Model.ModelID()
	startedAt := time.Now()
	response := &SpeechModelResponseMetadata{Timestamp: startedAt, ModelID: modelID}
	warningsResolved := false
	hasAudio := false
	finished := false
	var lastError interface{}

	var streamMu sync.Mutex
	var stream provider.SpeechStream
	closeStream := func() {
		streamMu.Lock()
		s := stream
		streamMu.Unlock()
		if s != nil {
			_ = s.Close()
		}
	}

	fail := func(err error) {
		result.setFinalErr(err)
		result.rejectAll(err)
		text.Cancel(err)
	}

	// A cancellation settles the result and errors the output immediately,
	// even while DoStreamSpeech is pending or the provider ignores the
	// context: pending accessors reject now and the provider stream closes.
	watcherDone := make(chan struct{})
	defer close(watcherDone)
	go func() {
		select {
		case <-pipeCtx.Done():
			select {
			case <-result.pipeDone:
				return
			default:
			}
			err := context.Cause(pipeCtx)
			fail(err)
			closeStream()
		case <-watcherDone:
		}
	}()

	streamResult, err := streamer.DoStreamSpeech(pipeCtx, &provider.SpeechStreamOptions{
		Text:             text,
		Voice:            opts.Voice,
		OutputFormat:     opts.OutputFormat,
		Instructions:     opts.Instructions,
		Speed:            opts.Speed,
		Language:         opts.Language,
		ProviderOptions:  opts.ProviderOptions,
		AbortSignal:      pipeCtx,
		Headers:          transcribeHeadersWithUserAgent(opts.Headers),
		IncludeRawChunks: opts.IncludeRawChunks,
	})
	if err != nil {
		fail(err)
		return
	}
	if streamResult == nil || streamResult.Stream == nil {
		fail(errors.New("speech model returned no stream"))
		return
	}
	streamMu.Lock()
	stream = streamResult.Stream
	streamMu.Unlock()
	defer closeStream()
	if pipeCtx.Err() != nil {
		closeStream() // cancelled while DoStreamSpeech was pending
	}

	if r := streamResult.Response; r != nil {
		response = &SpeechModelResponseMetadata{
			Timestamp: nonZeroTime(r.Timestamp, startedAt),
			ModelID:   nonEmptyString(r.ModelID, modelID),
			Headers:   r.Headers,
			Body:      r.Body,
		}
	}

	resolveWarnings := func(warnings []types.Warning) {
		warnings = warningsOrEmpty(warnings)
		result.warningsP.resolve(warnings)
		warningsResolved = true
		logModelWarnings(warnings, opts.Model.Provider(), modelID)
	}

	noSpeech := func() error {
		var cause error
		if lastError != nil {
			cause = speechErrorFromPayload(lastError)
		}
		return &NoSpeechGeneratedError{Responses: []SpeechModelResponseMetadata{*response}, Cause: cause}
	}

	emit := func(part SpeechStreamPart) bool {
		select {
		case result.ch <- part:
			return true
		case <-pipeCtx.Done():
			return false
		}
	}

	for {
		part, nextErr := stream.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			if pipeCtx.Err() != nil {
				nextErr = context.Cause(pipeCtx)
			}
			fail(nextErr)
			return
		}

		switch part.Type {
		case provider.SpeechStreamPartTypeStreamStart:
			resolveWarnings(part.Warnings)

		case provider.SpeechStreamPartTypeResponseMetadata:
			next := *response
			if !part.Timestamp.IsZero() {
				next.Timestamp = part.Timestamp
			}
			if part.ModelID != "" {
				next.ModelID = part.ModelID
			}
			if part.Headers != nil {
				next.Headers = part.Headers
			}
			if part.Body != nil {
				next.Body = part.Body
			}
			response = &next

		case provider.SpeechStreamPartTypeAudio:
			// Empty chunks are dropped unless they carry provider metadata
			// (e.g. an end-of-turn marker); those never count as audio.
			if len(part.Audio) == 0 && part.ProviderMetadata == nil {
				break
			}
			if len(part.Audio) > 0 {
				hasAudio = true
			}
			if !emit(SpeechStreamPart{
				Type:             provider.SpeechStreamPartTypeAudio,
				ID:               part.ID,
				Audio:            part.Audio,
				AudioFormat:      part.AudioFormat,
				ProviderMetadata: part.ProviderMetadata,
			}) {
				fail(context.Cause(pipeCtx))
				return
			}

		case provider.SpeechStreamPartTypeRaw:
			if !emit(SpeechStreamPart{Type: provider.SpeechStreamPartTypeRaw, RawValue: part.RawValue}) {
				fail(context.Cause(pipeCtx))
				return
			}

		case provider.SpeechStreamPartTypeError:
			lastError = part.Err
			go func(payload interface{}) {
				defer func() { _ = recover() }()
				onError(speechErrorFromPayload(payload))
			}(part.Err)
			if !emit(SpeechStreamPart{Type: provider.SpeechStreamPartTypeError, Err: part.Err}) {
				fail(context.Cause(pipeCtx))
				return
			}

		case provider.SpeechStreamPartTypeFinish:
			if !warningsResolved {
				resolveWarnings(nil)
			}
			if !hasAudio {
				fail(noSpeech())
				return
			}
			finished = true
			var usage SpeechStreamUsage
			if part.Usage != nil {
				usage = *part.Usage
			}
			providerMetadata := part.ProviderMetadata
			if providerMetadata == nil {
				providerMetadata = map[string]interface{}{}
			}
			result.usageP.resolve(usage)
			result.responsesP.resolve([]SpeechModelResponseMetadata{*response})
			result.providerMetadataP.resolve(providerMetadata)
		}
	}

	if !finished {
		// A stream that ends without a finish part: with no audio heard it is
		// a no-speech failure; after audio it surfaces the provider's error.
		if !hasAudio {
			fail(noSpeech())
		} else if lastError != nil {
			fail(speechErrorFromPayload(lastError))
		} else {
			fail(&providererrors.StreamProviderError{Message: "The speech stream ended before it finished."})
		}
	}
}
