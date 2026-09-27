package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
		return nil, errors.New("fullStream can only be accessed once.")
	case "result-promises":
		return nil, errors.New("fullStream cannot be accessed after a result promise.")
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

	go runTranscriptionStreamPipe(pipeCtx, streamer, opts, result)

	return result, nil
}

func runTranscriptionStreamPipe(pipeCtx context.Context, streamer provider.TranscriptionStreamer, opts StreamTranscribeOptions, result *StreamTranscriptionResult) {
	defer close(result.ch)

	startedAt := time.Now()
	response := &TranscriptionModelResponseMetadata{Timestamp: startedAt, ModelID: opts.Model.ModelID()}
	warningsResolved := false

	fail := func(err error) {
		result.setFinalErr(err)
		result.rejectAll(err)
		opts.Audio.Cancel(err)
	}

	streamResult, err := streamer.DoStream(pipeCtx, &provider.TranscriptionStreamOptions{
		Audio:            opts.Audio,
		InputAudioFormat: opts.InputAudioFormat,
		ProviderOptions:  opts.ProviderOptions,
		AbortSignal:      pipeCtx,
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
			if !sendTranscriptionPart(pipeCtx, result.ch, transcriptionPartFromProvider(part)) {
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
