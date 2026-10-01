package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SpeechTranslationModelResponseMetadata contains metadata for a speech
// translation model call.
type SpeechTranslationModelResponseMetadata struct {
	Timestamp time.Time         `json:"timestamp"`
	ModelID   string            `json:"modelId"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// NoTranslationGeneratedError is returned when a speech translation model
// produces neither audio output nor a non-empty output text.
type NoTranslationGeneratedError struct {
	Response SpeechTranslationModelResponseMetadata
}

func (e *NoTranslationGeneratedError) Error() string {
	return "No translation generated."
}

// IsNoTranslationGeneratedError reports whether err is a
// NoTranslationGeneratedError.
func IsNoTranslationGeneratedError(err error) bool {
	var target *NoTranslationGeneratedError
	return errors.As(err, &target)
}

// StreamTranslateOptions configures ExperimentalStreamTranslate.
type StreamTranslateOptions struct {
	// Model to use for speech-to-speech translation.
	Model provider.SpeechTranslationModel

	// Audio is the source of raw audio chunks to translate.
	Audio provider.AudioStream

	// InputAudioFormat is the input audio format for the raw audio chunks.
	InputAudioFormat provider.AudioFormat

	// TargetLanguage is the language to translate the audio into, as a
	// BCP-47-style language tag (e.g. "en", "es", "fr-CA").
	TargetLanguage string

	// SourceLanguage is the language of the source audio. Auto-detected when
	// empty.
	SourceLanguage string

	// OutputAudioFormat is the desired audio format for translated audio
	// chunks. When nil, the provider default output format is used.
	OutputAudioFormat *provider.AudioFormat

	// ProviderOptions contains provider-specific request options.
	ProviderOptions map[string]interface{}

	// Additional HTTP/WebSocket headers to send when supported by the provider.
	Headers map[string]string

	// IncludeRawChunks requests provider raw chunks in the stream.
	IncludeRawChunks bool
}

// TranslationStreamPart is one part of ExperimentalStreamTranslate's
// FullStream, mirroring TS TranslationStreamPart. stream-start,
// response-metadata and finish parts from the provider are consumed
// internally and surfaced via the result accessors instead of appearing here.
type TranslationStreamPart struct {
	// Type is one of "audio", "output-text-delta", "output-text-final",
	// "source-transcript-delta", "source-transcript-partial",
	// "source-transcript-final", "raw", or "error".
	Type string

	ID               string
	AudioData        []byte
	Delta            string
	Text             string
	StartSecond      *float64
	EndSecond        *float64
	ChannelIndex     *int
	ProviderMetadata map[string]interface{}

	// RawValue carries the provider's raw chunk when Type is "raw".
	RawValue interface{}

	// Err carries the streamed error payload when Type is "error".
	Err interface{}
}

// TranslationStream is a Next()-based stream of TranslationStreamPart,
// following the same convention as provider.TextStream.
type TranslationStream interface {
	Next() (*TranslationStreamPart, error)
	Err() error
	Close() error
}

// StreamTranslationResult is the result of ExperimentalStreamTranslate.
//
// FullStream is a single-consumer stream and can only be accessed once.
// Access it before any result accessor when both stream parts and final
// results are needed; calling a result accessor first consumes the stream
// internally and makes FullStream unavailable.
type StreamTranslationResult struct {
	sourceTextP       *streamPromise[string]
	translationTextP  *streamPromise[string]
	durationP         *streamPromise[*float64]
	usageP            *streamPromise[*provider.SpeechTranslationUsage]
	warningsP         *streamPromise[[]types.Warning]
	responseP         *streamPromise[SpeechTranslationModelResponseMetadata]
	providerMetadataP *streamPromise[map[string]interface{}]

	mu          sync.Mutex
	streamOwner string
	ch          chan TranslationStreamPart
	cancel      context.CancelFunc
	finalErr    *error
	finalErrMu  sync.Mutex
}

func (r *StreamTranslationResult) setFinalErr(err error) {
	r.finalErrMu.Lock()
	defer r.finalErrMu.Unlock()
	if r.finalErr == nil {
		r.finalErr = &err
	}
}

func (r *StreamTranslationResult) getFinalErr() error {
	r.finalErrMu.Lock()
	defer r.finalErrMu.Unlock()
	if r.finalErr == nil {
		return nil
	}
	return *r.finalErr
}

func (r *StreamTranslationResult) rejectAll(err error) {
	r.sourceTextP.reject(err)
	r.translationTextP.reject(err)
	r.durationP.reject(err)
	r.usageP.reject(err)
	r.warningsP.reject(err)
	r.responseP.reject(err)
	r.providerMetadataP.reject(err)
}

func (r *StreamTranslationResult) consumeStream() {
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

// SourceText returns the final source-language transcript of the input audio.
func (r *StreamTranslationResult) SourceText() (string, error) {
	r.consumeStream()
	return r.sourceTextP.wait()
}

// TranslationText returns the final translated text in the target language.
// May resolve to an empty string for providers that produce only audio
// output.
func (r *StreamTranslationResult) TranslationText() (string, error) {
	r.consumeStream()
	return r.translationTextP.wait()
}

// DurationInSeconds returns the duration of the source audio, if available.
func (r *StreamTranslationResult) DurationInSeconds() (*float64, error) {
	r.consumeStream()
	return r.durationP.wait()
}

// Usage returns usage information for the call, if reported by the provider.
func (r *StreamTranslationResult) Usage() (*provider.SpeechTranslationUsage, error) {
	r.consumeStream()
	return r.usageP.wait()
}

// Warnings returns warnings for the call, e.g. unsupported settings.
func (r *StreamTranslationResult) Warnings() ([]types.Warning, error) {
	r.consumeStream()
	return r.warningsP.wait()
}

// Response returns response metadata.
func (r *StreamTranslationResult) Response() (SpeechTranslationModelResponseMetadata, error) {
	r.consumeStream()
	return r.responseP.wait()
}

// ProviderMetadata returns additional provider-specific metadata.
func (r *StreamTranslationResult) ProviderMetadata() (map[string]interface{}, error) {
	r.consumeStream()
	return r.providerMetadataP.wait()
}

// FullStream returns the full stream of translation parts. It can only be
// accessed once, and only before any result accessor has been called.
func (r *StreamTranslationResult) FullStream() (TranslationStream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.streamOwner {
	case "full-stream":
		return nil, errors.New("fullStream can only be accessed once.") //nolint:staticcheck // matches TS SDK's exact error text
	case "result-promises":
		return nil, errors.New("fullStream cannot be accessed after a result promise.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	r.streamOwner = "full-stream"
	return &translationFullStream{result: r}, nil
}

type translationFullStream struct {
	result    *StreamTranslationResult
	err       error
	closeOnce sync.Once
}

func (s *translationFullStream) Next() (*TranslationStreamPart, error) {
	part, ok := <-s.result.ch
	if !ok {
		if err := s.result.getFinalErr(); err != nil {
			s.err = err
			return nil, err
		}
		return nil, io.EOF
	}
	return &part, nil
}

func (s *translationFullStream) Err() error { return s.err }

func (s *translationFullStream) Close() error {
	s.closeOnce.Do(func() {
		s.result.cancel()
	})
	return nil
}

// ExperimentalStreamTranslate streams speech-to-speech translations using a
// provider.SpeechTranslationModel.
//
// Result accessors resolve without the caller needing to consume FullStream:
// calling any accessor drains the stream internally. Cancelling FullStream
// (Close) aborts a still-pending DoStream setup or an in-flight provider
// stream; failures cancel the caller's Audio stream.
func ExperimentalStreamTranslate(ctx context.Context, opts StreamTranslateOptions) (*StreamTranslationResult, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	pipeCtx, cancel := context.WithCancel(ctx)

	result := &StreamTranslationResult{
		sourceTextP:       newStreamPromise[string](),
		translationTextP:  newStreamPromise[string](),
		durationP:         newStreamPromise[*float64](),
		usageP:            newStreamPromise[*provider.SpeechTranslationUsage](),
		warningsP:         newStreamPromise[[]types.Warning](),
		responseP:         newStreamPromise[SpeechTranslationModelResponseMetadata](),
		providerMetadataP: newStreamPromise[map[string]interface{}](),
		streamOwner:       "unclaimed",
		ch:                make(chan TranslationStreamPart),
		cancel:            cancel,
	}

	go runTranslationStreamPipe(pipeCtx, opts, result)

	return result, nil
}

func runTranslationStreamPipe(pipeCtx context.Context, opts StreamTranslateOptions, result *StreamTranslationResult) {
	defer close(result.ch)

	startedAt := time.Now()
	response := SpeechTranslationModelResponseMetadata{Timestamp: startedAt, ModelID: opts.Model.ModelID()}
	warningsResolved := false
	hasAudioOutput := false

	fail := func(err error) {
		result.setFinalErr(err)
		result.rejectAll(err)
		opts.Audio.Cancel(err)
	}

	streamResult, err := opts.Model.DoStream(pipeCtx, &provider.SpeechTranslationStreamOptions{
		Audio:             opts.Audio,
		InputAudioFormat:  opts.InputAudioFormat,
		TargetLanguage:    opts.TargetLanguage,
		SourceLanguage:    opts.SourceLanguage,
		OutputAudioFormat: opts.OutputAudioFormat,
		ProviderOptions:   opts.ProviderOptions,
		AbortSignal:       pipeCtx,
		Headers:           transcribeHeadersWithUserAgent(opts.Headers),
		IncludeRawChunks:  opts.IncludeRawChunks,
	})
	if err != nil {
		fail(err)
		return
	}
	if streamResult.Response != nil {
		response = SpeechTranslationModelResponseMetadata{
			Timestamp: nonZeroTime(streamResult.Response.Timestamp, startedAt),
			ModelID:   nonEmptyString(streamResult.Response.ModelID, opts.Model.ModelID()),
			Headers:   streamResult.Response.Headers,
		}
	}
	defer streamResult.Stream.Close() //nolint:errcheck

	resolveWarnings := func(warnings []types.Warning) {
		if warnings == nil {
			warnings = []types.Warning{}
		}
		result.warningsP.resolve(warnings)
		warningsResolved = true
		logModelWarnings(warnings, opts.Model.Provider(), opts.Model.ModelID())
	}

	for {
		part, nextErr := streamResult.Stream.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			fail(nextErr)
			return
		}

		switch part.Type {
		case provider.SpeechTranslationStreamPartTypeStreamStart:
			resolveWarnings(part.Warnings)

		case provider.SpeechTranslationStreamPartTypeResponseMetadata:
			ts := response.Timestamp
			if !part.Timestamp.IsZero() {
				ts = part.Timestamp
			}
			modelID := response.ModelID
			if part.ModelID != "" {
				modelID = part.ModelID
			}
			headers := response.Headers
			if part.Headers != nil {
				headers = part.Headers
			}
			response = SpeechTranslationModelResponseMetadata{Timestamp: ts, ModelID: modelID, Headers: headers}

		case provider.SpeechTranslationStreamPartTypeAudio:
			hasAudioOutput = true
			if !sendTranslationPart(pipeCtx, result.ch, translationPartFromProvider(part)) {
				// See the matching comment in runTranscriptionStreamPipe: a
				// pending send that loses to pipeCtx cancellation must still
				// reject the result promises and cancel the caller's audio,
				// mirroring TS stream-translate.ts's Transformer.cancel/catch.
				fail(errors.New("Translation stream was cancelled.")) //nolint:staticcheck // matches TS SDK's exact error text
				return
			}

		case provider.SpeechTranslationStreamPartTypeOutputTextDelta,
			provider.SpeechTranslationStreamPartTypeOutputTextFinal,
			provider.SpeechTranslationStreamPartTypeSourceTranscriptDelta,
			provider.SpeechTranslationStreamPartTypeSourceTranscriptPartial,
			provider.SpeechTranslationStreamPartTypeSourceTranscriptFinal,
			provider.SpeechTranslationStreamPartTypeRaw,
			provider.SpeechTranslationStreamPartTypeError:
			if !sendTranslationPart(pipeCtx, result.ch, translationPartFromProvider(part)) {
				fail(errors.New("Translation stream was cancelled.")) //nolint:staticcheck // matches TS SDK's exact error text
				return
			}

		case provider.SpeechTranslationStreamPartTypeFinish:
			if !warningsResolved {
				resolveWarnings(nil)
			}
			if !hasAudioOutput && part.OutputText == "" {
				fail(&NoTranslationGeneratedError{Response: response})
				return
			}
			providerMetadata := part.ProviderMetadata
			if providerMetadata == nil {
				providerMetadata = map[string]interface{}{}
			}
			result.sourceTextP.resolve(part.SourceText)
			result.translationTextP.resolve(part.OutputText)
			result.durationP.resolve(part.DurationInSeconds)
			result.usageP.resolve(part.Usage)
			result.responseP.resolve(response)
			result.providerMetadataP.resolve(providerMetadata)

		default:
			// Mirrors TS stream-translate.ts's exhaustive-check default
			// branch: an unrecognized part type from the provider is a hard
			// error rather than a silently dropped chunk.
			fail(fmt.Errorf("unsupported part type: %s", part.Type))
			return
		}
	}

	if result.translationTextP.isPending() {
		fail(&NoTranslationGeneratedError{Response: response})
	}
}

func translationPartFromProvider(part *provider.SpeechTranslationStreamPart) TranslationStreamPart {
	return TranslationStreamPart{
		Type:             part.Type,
		ID:               part.ID,
		AudioData:        part.AudioData,
		Delta:            part.Delta,
		Text:             part.Text,
		StartSecond:      part.StartSecond,
		EndSecond:        part.EndSecond,
		ChannelIndex:     part.ChannelIndex,
		ProviderMetadata: part.ProviderMetadata,
		RawValue:         part.RawValue,
		Err:              part.Err,
	}
}

func sendTranslationPart(ctx context.Context, ch chan<- TranslationStreamPart, part TranslationStreamPart) bool {
	select {
	case ch <- part:
		return true
	case <-ctx.Done():
		return false
	}
}
