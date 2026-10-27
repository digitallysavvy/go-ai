package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// ErrStreamAlreadyConsumed is returned when a second stream accessor or
// response helper is used on a single-consumer result (TS
// AI_StreamAlreadyConsumedError). Test with errors.Is.
var ErrStreamAlreadyConsumed = errors.New("AI_StreamAlreadyConsumedError: This result supports one stream consumer. Choose one stream projection or response helper.") //nolint:staticcheck // matches TS SDK's exact error text

// alreadyConsumedStream is the stream a second claim receives.
type alreadyConsumedStream struct{}

func (alreadyConsumedStream) Next() (*provider.StreamChunk, error) {
	return nil, ErrStreamAlreadyConsumed
}
func (alreadyConsumedStream) Err() error   { return ErrStreamAlreadyConsumed }
func (alreadyConsumedStream) Close() error { return nil }

// StreamTextSingleConsumerResult is a single-consumer view of a text
// generation: it has no replay and none of the final-content accessors (Text,
// Content, Steps, FinalStep, ResponseMessages, Output, ...). Use StreamText
// when you need them. Experimental (TS experimental_streamTextLowMemory,
// #22528, #23555).
//
// Choose ONE stream accessor, response helper or ConsumeStream: a second
// attempt fails with ErrStreamAlreadyConsumed. Completion metadata
// (FinishReason, Usage, ...) does not consume the stream and blocks until the
// generation finished, so start a consumer first: the producer waits for the
// consumer (backpressure). Close cancels the generation.
type StreamTextSingleConsumerResult struct {
	r       *StreamTextResult
	mu      sync.Mutex
	claimed bool
}

// StreamTextLowMemory streams text with a single consumer, without a replay
// buffer or retained final content, to reduce memory use. It accepts the same
// options as StreamText and uses the same execution engine. Content is still
// collected where its contract needs it: tools, tool choice, structured
// output, PrepareStep, stop conditions, completion callbacks and telemetry.
// Plain text streaming does not accumulate the generated text.
func StreamTextLowMemory(ctx context.Context, opts StreamTextOptions) (*StreamTextSingleConsumerResult, error) {
	opts.lowMemory = true
	opts.collectContent = lowMemoryNeedsContent(opts)
	r, err := StreamText(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &StreamTextSingleConsumerResult{r: r}, nil
}

// lowMemoryNeedsContent mirrors the TS collectContent decision: content is
// needed by tools, structured output, step control and full-content callbacks
// or telemetry.
func lowMemoryNeedsContent(opts StreamTextOptions) bool {
	telemetryOn := false
	settings := opts.Telemetry
	if settings == nil {
		settings = opts.ExperimentalTelemetry
	}
	if settings == nil || telemetry.Enabled(settings) {
		telemetryOn = telemetry.HasIntegrations() || (settings != nil && len(settings.Integrations) > 0)
	}
	return len(opts.Tools) > 0 || opts.ToolChoice.Type != "" || opts.Output != nil ||
		opts.PrepareStep != nil || len(opts.StopWhen) > 0 || opts.MaxSteps != nil ||
		opts.OnEnd != nil || opts.OnFinish != nil || opts.OnEndEvent != nil || opts.OnFinishEvent != nil ||
		opts.OnStepEnd != nil || opts.OnStepFinish != nil || opts.OnStepEndEvent != nil || opts.OnStepFinishEvent != nil ||
		opts.OnAbort != nil || opts.OnAbortEvent != nil ||
		opts.OnLanguageModelCallEnd != nil || opts.ExperimentalOnLanguageModelCallEnd != nil ||
		telemetryOn
}

// Stream claims the only cursor over the full chunk stream.
func (s *StreamTextSingleConsumerResult) Stream() (provider.TextStream, error) {
	if err := s.claim(); err != nil {
		return nil, err
	}
	return &singleConsumerStream{TextStream: s.r.Stream(), result: s.r}, nil
}

// claim reserves the only consumer; it fails when one was already chosen.
func (s *StreamTextSingleConsumerResult) claim() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimed {
		return ErrStreamAlreadyConsumed
	}
	s.claimed = true
	return nil
}

// singleConsumerStream closes the whole generation when the consumer closes.
type singleConsumerStream struct {
	provider.TextStream
	result *StreamTextResult
}

func (s *singleConsumerStream) Close() error {
	err := s.TextStream.Close()
	if cerr := s.result.Close(); err == nil {
		err = cerr
	}
	return err
}

// ConsumeStream claims the stream, discards it, and waits for completion.
func (s *StreamTextSingleConsumerResult) ConsumeStream() error {
	stream, err := s.Stream()
	if err != nil {
		return err
	}
	for {
		if _, nerr := stream.Next(); nerr != nil {
			break
		}
	}
	_ = stream.Close()
	_, err = s.r.ReadAll()
	return err
}

// ToUIMessageStream claims the stream and converts it to UI message chunks.
func (s *StreamTextSingleConsumerResult) ToUIMessageStream(ctx context.Context, opts ...UIMessageStreamResultOptions) (<-chan UIMessageChunk, <-chan error, error) {
	if err := s.claim(); err != nil {
		return nil, nil, err
	}
	chunks, errs := s.r.ToUIMessageStream(ctx, opts...) //nolint:staticcheck // the result's own converter
	return chunks, errs, nil
}

// ToUIMessageStreamResponse claims the stream and returns an SSE response.
func (s *StreamTextSingleConsumerResult) ToUIMessageStreamResponse(ctx context.Context, init *UIMessageStreamResponseInit, opts ...UIMessageStreamResultOptions) (*http.Response, error) {
	if err := s.claim(); err != nil {
		return nil, err
	}
	return s.r.ToUIMessageStreamResponse(ctx, init, opts...) //nolint:staticcheck // the result's own converter
}

// PipeUIMessageStreamToResponse claims the stream and writes UI message chunks.
func (s *StreamTextSingleConsumerResult) PipeUIMessageStreamToResponse(ctx context.Context, w io.Writer, opts ...UIMessageStreamResultOptions) error {
	if err := s.claim(); err != nil {
		return err
	}
	return s.r.PipeUIMessageStreamToResponse(ctx, w, opts...) //nolint:staticcheck // the result's own converter
}

// ToTextStreamResponse claims the stream and returns a text response.
func (s *StreamTextSingleConsumerResult) ToTextStreamResponse(ctx context.Context, init *TextStreamResponseInit) (*http.Response, error) {
	if err := s.claim(); err != nil {
		return nil, err
	}
	return s.r.ToTextStreamResponse(ctx, init) //nolint:staticcheck // the result's own converter
}

// PipeTextStreamToResponse claims the stream and writes the text deltas.
func (s *StreamTextSingleConsumerResult) PipeTextStreamToResponse(ctx context.Context, w io.Writer) error {
	if err := s.claim(); err != nil {
		return err
	}
	return s.r.PipeTextStreamToResponse(ctx, w) //nolint:staticcheck // the result's own converter
}

// FinishReason waits for the generation to finish and returns why it ended.
// It does not consume the stream.
func (s *StreamTextSingleConsumerResult) FinishReason() types.FinishReason { return s.r.FinishReason() }

// RawFinishReason waits for the generation to finish and returns the raw
// provider finish reason.
func (s *StreamTextSingleConsumerResult) RawFinishReason() string { return s.r.RawFinishReason() }

// Usage waits for the generation to finish and returns the last step's usage.
func (s *StreamTextSingleConsumerResult) Usage() types.Usage { return s.r.Usage() }

// TotalUsage waits for the generation to finish and returns the usage summed
// over all steps.
func (s *StreamTextSingleConsumerResult) TotalUsage() types.Usage { return s.r.TotalUsage() }

// PartialOutput returns the most recently parsed partial structured output
// (nil without an Output option). It does not consume the stream and is safe
// to call while streaming (TS partialOutputStream, as a snapshot like
// StreamTextResult.PartialOutput).
func (s *StreamTextSingleConsumerResult) PartialOutput() any { return s.r.PartialOutput() }

// ElementStreamSingleConsumer claims the stream and publishes the elements of
// a streamed array output as they complete (TS elementStream). It is
// ElementStream for a single-consumer result.
func ElementStreamSingleConsumer[ELEMENT any](s *StreamTextSingleConsumerResult, opts ElementStreamOptions[ELEMENT]) (<-chan ElementStreamResult[ELEMENT], error) {
	if err := s.claim(); err != nil {
		return nil, err
	}
	return ElementStream[ELEMENT](s.r, opts), nil
}

// Err waits for the generation to finish and returns its error, if any.
func (s *StreamTextSingleConsumerResult) Err() error { return s.r.Err() }

// Close cancels the generation and releases the producer.
func (s *StreamTextSingleConsumerResult) Close() error { return s.r.Close() }
