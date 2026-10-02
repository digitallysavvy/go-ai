package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// StreamTranscribeOptions configures ExperimentalStreamTranscribe.
type StreamTranscribeOptions struct {
	// Model must implement provider.TranscriptionStreamer.
	Model provider.TranscriptionModel

	// Audio is the source of raw audio chunks to transcribe.
	Audio provider.AudioStream

	// InputAudioFormat is the input audio format for the raw audio chunks.
	InputAudioFormat provider.AudioFormat

	// ProviderOptions contains provider-specific request options.
	ProviderOptions map[string]interface{}

	// Additional HTTP/WebSocket headers to send when supported by the provider.
	Headers map[string]string

	// IncludeRawChunks requests provider raw chunks in the stream.
	IncludeRawChunks bool

	// Telemetry configures observability for this operation.
	// When both Telemetry and ExperimentalTelemetry are set, Telemetry wins.
	Telemetry *TelemetrySettings

	// ExperimentalTelemetry configures observability for this operation.
	//
	// Deprecated: use Telemetry.
	ExperimentalTelemetry *TelemetrySettings
}

// TranscriptionStreamPart is one part of ExperimentalStreamTranscribe's
// FullStream, mirroring TS TranscriptionStreamPart. stream-start,
// response-metadata and finish parts from the provider are consumed
// internally and surfaced via the result accessors instead of appearing here.
type TranscriptionStreamPart struct {
	// Type is one of "transcript-delta", "transcript-partial",
	// "transcript-final", "raw", or "error".
	Type string

	ID                string
	Delta             string
	Text              string
	StartSecond       *float64
	EndSecond         *float64
	DurationInSeconds *float64
	ChannelIndex      *int
	ProviderMetadata  map[string]interface{}

	// RawValue carries the provider's raw chunk when Type is "raw".
	RawValue interface{}

	// Err carries the streamed error payload when Type is "error".
	Err interface{}
}

// StreamTranscriptionResult is the result of ExperimentalStreamTranscribe.
//
// FullStream is a single-consumer stream and can only be accessed once.
// Access it before any result accessor when both stream parts and final
// results are needed; calling a result accessor first consumes the stream
// internally and makes FullStream unavailable.
type StreamTranscriptionResult struct {
	textP             *streamPromise[string]
	segmentsP         *streamPromise[[]TranscriptionSegment]
	languageP         *streamPromise[string]
	durationP         *streamPromise[*float64]
	warningsP         *streamPromise[[]types.Warning]
	responsesP        *streamPromise[[]TranscriptionModelResponseMetadata]
	providerMetadataP *streamPromise[map[string]interface{}]

	mu          sync.Mutex
	streamOwner string // "unclaimed" | "full-stream" | "result-promises"
	ch          chan TranscriptionStreamPart
	cancel      context.CancelFunc
	finalErr    *error
	finalErrMu  sync.Mutex
}

func (r *StreamTranscriptionResult) setFinalErr(err error) {
	r.finalErrMu.Lock()
	defer r.finalErrMu.Unlock()
	if r.finalErr == nil {
		r.finalErr = &err
	}
}

func (r *StreamTranscriptionResult) getFinalErr() error {
	r.finalErrMu.Lock()
	defer r.finalErrMu.Unlock()
	if r.finalErr == nil {
		return nil
	}
	return *r.finalErr
}

func (r *StreamTranscriptionResult) rejectAll(err error) {
	r.textP.reject(err)
	r.segmentsP.reject(err)
	r.languageP.reject(err)
	r.durationP.reject(err)
	r.warningsP.reject(err)
	r.responsesP.reject(err)
	r.providerMetadataP.reject(err)
}

func (r *StreamTranscriptionResult) consumeStream() {
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

// Text returns the final transcribed text, blocking until available.
func (r *StreamTranscriptionResult) Text() (string, error) {
	r.consumeStream()
	return r.textP.wait()
}

// Segments returns final transcript segments with timing information.
func (r *StreamTranscriptionResult) Segments() ([]TranscriptionSegment, error) {
	r.consumeStream()
	return r.segmentsP.wait()
}

// Language returns the language of the transcript, if available.
func (r *StreamTranscriptionResult) Language() (string, error) {
	r.consumeStream()
	return r.languageP.wait()
}

// DurationInSeconds returns the duration of the transcript, if available.
func (r *StreamTranscriptionResult) DurationInSeconds() (*float64, error) {
	r.consumeStream()
	return r.durationP.wait()
}

// Warnings returns warnings for the call, e.g. unsupported settings.
func (r *StreamTranscriptionResult) Warnings() ([]types.Warning, error) {
	r.consumeStream()
	return r.warningsP.wait()
}

// Responses returns response metadata.
func (r *StreamTranscriptionResult) Responses() ([]TranscriptionModelResponseMetadata, error) {
	r.consumeStream()
	return r.responsesP.wait()
}

// ProviderMetadata returns additional provider-specific metadata.
func (r *StreamTranscriptionResult) ProviderMetadata() (map[string]interface{}, error) {
	r.consumeStream()
	return r.providerMetadataP.wait()
}

// FullStream returns the full stream of transcription parts. It can only be
// accessed once, and only before any result accessor has been called.
func (r *StreamTranscriptionResult) FullStream() (TranscriptionStream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch r.streamOwner {
	case "full-stream":
		return nil, errors.New("fullStream can only be accessed once.") //nolint:staticcheck // matches TS SDK's exact error text
	case "result-promises":
		return nil, errors.New("fullStream cannot be accessed after a result promise.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	r.streamOwner = "full-stream"
	return &transcriptionFullStream{result: r}, nil
}

// TranscriptionStream is a Next()-based stream of TranscriptionStreamPart,
// following the same convention as provider.TextStream.
type TranscriptionStream interface {
	Next() (*TranscriptionStreamPart, error)
	Err() error
	Close() error
}

type transcriptionFullStream struct {
	result    *StreamTranscriptionResult
	err       error
	closeOnce sync.Once
}

func (s *transcriptionFullStream) Next() (*TranscriptionStreamPart, error) {
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

func (s *transcriptionFullStream) Err() error { return s.err }

func (s *transcriptionFullStream) Close() error {
	s.closeOnce.Do(func() {
		s.result.cancel()
	})
	return nil
}

// ExperimentalStreamTranscribe streams transcripts using a transcription
// model that implements provider.TranscriptionStreamer.
//
// Result accessors (Text, Segments, ...) resolve without the caller needing
// to consume FullStream: calling any accessor drains the stream internally.
// Cancelling FullStream (Close) aborts a still-pending DoStream setup or an
// in-flight provider stream; failures cancel the caller's Audio stream.
func ExperimentalStreamTranscribe(ctx context.Context, opts StreamTranscribeOptions) (*StreamTranscriptionResult, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	streamer, ok := opts.Model.(provider.TranscriptionStreamer)
	if !ok {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "streaming transcription",
			Message: fmt.Sprintf(
				"The %s model %q does not support streaming transcription.",
				opts.Model.Provider(), opts.Model.ModelID(),
			),
		}
	}
	opts.ExperimentalTelemetry = effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)
	callID := newCallID()

	pipeCtx, cancel := context.WithCancel(ctx)

	result := &StreamTranscriptionResult{
		textP:             newStreamPromise[string](),
		segmentsP:         newStreamPromise[[]TranscriptionSegment](),
		languageP:         newStreamPromise[string](),
		durationP:         newStreamPromise[*float64](),
		warningsP:         newStreamPromise[[]types.Warning](),
		responsesP:        newStreamPromise[[]TranscriptionModelResponseMetadata](),
		providerMetadataP: newStreamPromise[map[string]interface{}](),
		streamOwner:       "unclaimed",
		ch:                make(chan TranscriptionStreamPart),
		cancel:            cancel,
	}

	go runTranscriptionStreamPipe(pipeCtx, streamer, opts, callID, result)

	return result, nil
}

// telemetryCountingAudioStream wraps a provider.AudioStream, accumulating the
// byte length of every chunk read into total (mirrors TS stream-transcribe.ts
// createByteCountingStream, used to populate the "ai.streamTranscribe"
// StreamTranscriptionEndEvent's audio.byteLength). total is only read after
// the wrapped Next() calls have all returned (from runTranscriptionStreamPipe
// and whatever goroutine(s) the provider's DoStream uses internally to drain
// it), so atomic access guards against a provider that reads audio
// concurrently with telemetry inspecting the running total.
type telemetryCountingAudioStream struct {
	inner provider.AudioStream
	total *int64
}

func (s telemetryCountingAudioStream) Next(ctx context.Context) ([]byte, error) {
	chunk, err := s.inner.Next(ctx)
	if len(chunk) > 0 {
		atomic.AddInt64(s.total, int64(len(chunk)))
	}
	return chunk, err
}

func (s telemetryCountingAudioStream) Cancel(reason error) { s.inner.Cancel(reason) }

func runTranscriptionStreamPipe(pipeCtx context.Context, streamer provider.TranscriptionStreamer, opts StreamTranscribeOptions, callID string, result *StreamTranscriptionResult) {
	defer close(result.ch)

	startedAt := time.Now()
	response := &TranscriptionModelResponseMetadata{Timestamp: startedAt, ModelID: opts.Model.ModelID()}
	warningsResolved := false
	telemetrySettled := false

	var audioByteLength int64
	audio := opts.Audio
	if telemetry.Enabled(opts.ExperimentalTelemetry) {
		audio = telemetryCountingAudioStream{inner: opts.Audio, total: &audioByteLength}
	}

	ctx := telemetry.FireOnStart(pipeCtx, telemetry.TelemetryStartEvent{
		CallID:         callID,
		OperationType:  "ai.streamTranscribe",
		ModelProvider:  opts.Model.Provider(),
		ModelID:        opts.Model.ModelID(),
		Settings:       opts.ExperimentalTelemetry,
		AudioMediaType: opts.InputAudioFormat.Type,
		Headers:        opts.Headers,
	})

	fail := func(err error) {
		// Settle telemetry before rejecting result promises, for the same
		// race-avoidance reason as the "finish" case below: a goroutine
		// blocked in Text()/etc must never observe the error before the span
		// has already been ended.
		if !telemetrySettled {
			telemetrySettled = true
			telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{CallID: callID, Settings: opts.ExperimentalTelemetry, Error: err})
		}
		result.setFinalErr(err)
		result.rejectAll(err)
		opts.Audio.Cancel(err)
	}

	streamResult, err := streamer.DoStream(ctx, &provider.TranscriptionStreamOptions{
		Audio:            audio,
		InputAudioFormat: opts.InputAudioFormat,
		ProviderOptions:  opts.ProviderOptions,
		AbortSignal:      ctx,
		Headers:          transcribeHeadersWithUserAgent(opts.Headers),
		IncludeRawChunks: opts.IncludeRawChunks,
	})
	if err != nil {
		fail(err)
		return
	}
	if streamResult.Response != nil {
		response = &TranscriptionModelResponseMetadata{
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
		case provider.TranscriptionStreamPartTypeStreamStart:
			resolveWarnings(part.Warnings)

		case provider.TranscriptionStreamPartTypeResponseMetadata:
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
			response = &TranscriptionModelResponseMetadata{Timestamp: ts, ModelID: modelID, Headers: headers, Body: part.Body}

		case provider.TranscriptionStreamPartTypeDelta,
			provider.TranscriptionStreamPartTypePartial,
			provider.TranscriptionStreamPartTypeFinal,
			provider.TranscriptionStreamPartTypeRaw,
			provider.TranscriptionStreamPartTypeError:
			if !sendTranscriptionPart(ctx, result.ch, transcriptionPartFromProvider(part)) {
				// ctx (derived from pipeCtx by FireOnStart) was cancelled
				// while a send was pending (FullStream was closed, or the
				// caller's ctx was cancelled) while parts were still
				// flowing. Mirror the TS SDK's Transformer.cancel
				// path: reject the still-pending result promises and cancel
				// the caller's audio stream instead of leaving them to block
				// forever (TS stream-transcribe.ts cancel()/catch()).
				fail(errors.New("Transcription stream was cancelled.")) //nolint:staticcheck // matches TS SDK's exact error text
				return
			}

		case provider.TranscriptionStreamPartTypeFinish:
			if !warningsResolved {
				resolveWarnings(nil)
			}
			if part.FinishText == "" {
				fail(&NoTranscriptGeneratedError{Responses: []TranscriptionModelResponseMetadata{*response}})
				return
			}
			segments := make([]TranscriptionSegment, 0, len(part.Segments))
			for _, seg := range part.Segments {
				segments = append(segments, TranscriptionSegment{Text: seg.Text, StartSecond: seg.StartSecond, EndSecond: seg.EndSecond})
			}
			providerMetadata := part.ProviderMetadata
			if providerMetadata == nil {
				providerMetadata = map[string]interface{}{}
			}
			// Settle telemetry (ending the OTel span synchronously inside
			// FireOnEnd) before resolving any result promise. Unlike TS —
			// where a single-threaded microtask queue makes resolve-then-
			// notify safe because nothing else can run until the producer
			// yields — resolving first here would let a goroutine blocked in
			// Text()/etc race the span-ending code below, which the harness
			// explicitly calls out as a bug class to avoid.
			telemetrySettled = true
			finalAudioByteLength := atomic.LoadInt64(&audioByteLength)
			telemetry.FireOnEnd(ctx, telemetry.TelemetryFinishEvent{
				CallID:           callID,
				OperationType:    "ai.streamTranscribe",
				ModelProvider:    opts.Model.Provider(),
				ModelID:          opts.Model.ModelID(),
				Settings:         opts.ExperimentalTelemetry,
				Text:             part.FinishText,
				AudioByteLength:  &finalAudioByteLength,
				AudioMediaType:   opts.InputAudioFormat.Type,
				ProviderMetadata: providerMetadata,
				ProviderUsage:    part.Usage,
			})

			result.textP.resolve(part.FinishText)
			result.segmentsP.resolve(segments)
			result.languageP.resolve(part.Language)
			result.durationP.resolve(part.DurationInSeconds)
			result.responsesP.resolve([]TranscriptionModelResponseMetadata{*response})
			result.providerMetadataP.resolve(providerMetadata)
		}
	}

	if result.textP.isPending() {
		fail(&NoTranscriptGeneratedError{Responses: []TranscriptionModelResponseMetadata{*response}})
	}
}

func transcriptionPartFromProvider(part *provider.TranscriptionStreamPart) TranscriptionStreamPart {
	return TranscriptionStreamPart{
		Type:              part.Type,
		ID:                part.ID,
		Delta:             part.Delta,
		Text:              part.Text,
		StartSecond:       part.StartSecond,
		EndSecond:         part.EndSecond,
		DurationInSeconds: part.DurationInSeconds,
		ChannelIndex:      part.ChannelIndex,
		ProviderMetadata:  part.ProviderMetadata,
		RawValue:          part.RawValue,
		Err:               part.Err,
	}
}

// sendTranscriptionPart sends part on ch, honoring cancellation of ctx so a
// blocked send unblocks (instead of leaking) when FullStream is closed.
// Returns false when ctx was cancelled before the send completed.
func sendTranscriptionPart(ctx context.Context, ch chan<- TranscriptionStreamPart, part TranscriptionStreamPart) bool {
	select {
	case ch <- part:
		return true
	case <-ctx.Done():
		return false
	}
}

func nonZeroTime(t, fallback time.Time) time.Time {
	if t.IsZero() {
		return fallback
	}
	return t
}

func nonEmptyString(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
