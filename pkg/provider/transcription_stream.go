package provider

import (
	"context"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// AudioFormat describes the input/output audio format for streaming
// transcription and speech translation calls (TS SharedV4AudioFormat).
type AudioFormat struct {
	// Type is the raw audio encoding, e.g. "audio/pcm".
	Type string

	// Rate is the sample rate in Hz, when applicable.
	Rate *int
}

// AudioStream is a producer-controlled source of raw audio chunks for
// streaming transcription/translation calls. It mirrors the semantics of a
// TS ReadableStream<Uint8Array | string>: Next returns chunks until io.EOF,
// and Cancel notifies the producer that consumption failed or was abandoned
// (idempotent; reason may be nil), matching TS `audio.cancel(reason)`.
type AudioStream interface {
	// Next returns the next chunk of raw audio bytes, or io.EOF when the
	// producer has no more audio.
	Next(ctx context.Context) ([]byte, error)

	// Cancel notifies the producer that consumption failed or was
	// abandoned. Safe to call multiple times.
	Cancel(reason error)
}

// Transcription stream part types, mirroring TS
// Experimental_TranscriptionModelV4StreamPart's discriminated `type` field.
const (
	TranscriptionStreamPartTypeStreamStart      = "stream-start"
	TranscriptionStreamPartTypeDelta            = "transcript-delta"
	TranscriptionStreamPartTypePartial          = "transcript-partial"
	TranscriptionStreamPartTypeFinal            = "transcript-final"
	TranscriptionStreamPartTypeResponseMetadata = "response-metadata"
	TranscriptionStreamPartTypeFinish           = "finish"
	TranscriptionStreamPartTypeRaw              = "raw"
	TranscriptionStreamPartTypeError            = "error"
)

// TranscriptSegment is a finished transcript segment with timing information
// (TS's inline segment shape on the `finish` stream part).
type TranscriptSegment struct {
	Text        string
	StartSecond float64
	EndSecond   float64
}

// TranscriptionStreamPart is one part of a transcription model's stream,
// mirroring TS Experimental_TranscriptionModelV4StreamPart. Fields are
// grouped by which Type they apply to, following the same pattern as
// provider.StreamChunk.
type TranscriptionStreamPart struct {
	Type string

	// stream-start
	Warnings []types.Warning

	// transcript-delta / transcript-partial / transcript-final
	ID                string
	Delta             string
	Text              string
	StartSecond       *float64
	EndSecond         *float64
	DurationInSeconds *float64
	ChannelIndex      *int
	ProviderMetadata  map[string]interface{}

	// response-metadata
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}

	// finish
	FinishText string
	Segments   []TranscriptSegment
	Language   string

	// raw
	RawValue interface{}

	// error
	Err interface{}
}

// TranscriptionStream is a Next()-based stream of TranscriptionStreamPart,
// following the same convention as provider.TextStream: Next returns io.EOF
// when the stream is complete, Err reports any error, and Close releases
// resources (safe to call multiple times).
type TranscriptionStream interface {
	Next() (*TranscriptionStreamPart, error)
	Err() error
	Close() error
}

// TranscriptionStreamOptions contains options for
// TranscriptionStreamer.DoStream (TS
// Experimental_TranscriptionModelV4StreamOptions).
type TranscriptionStreamOptions struct {
	// Audio is the source of raw audio chunks to transcribe.
	Audio AudioStream

	// InputAudioFormat is the input audio format for the raw audio chunks.
	InputAudioFormat AudioFormat

	// ProviderOptions contains provider-specific request options.
	ProviderOptions map[string]interface{}

	// AbortSignal for cancellation.
	AbortSignal context.Context

	// Additional HTTP/WebSocket headers.
	Headers map[string]string

	// IncludeRawChunks requests provider raw chunks in the stream when
	// supported.
	IncludeRawChunks bool
}

// TranscriptionStreamResponseMetadata is optional response metadata returned
// by TranscriptionStreamer.DoStream / SpeechTranslationModel.DoStream.
type TranscriptionStreamResponseMetadata struct {
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}
}

// TranscriptionStreamResult is the result of a
// TranscriptionStreamer.DoStream call (TS
// Experimental_TranscriptionModelV4StreamResult).
type TranscriptionStreamResult struct {
	// Stream is the transcription stream.
	Stream TranscriptionStream

	// RequestBody is the request body or setup payload sent to the provider,
	// for telemetry/debugging.
	RequestBody interface{}

	// Response is optional response data.
	Response *TranscriptionStreamResponseMetadata
}

// TranscriptionStreamer is an optional capability implemented by
// transcription models that support streaming live audio (TS
// TranscriptionModelV4.doStream). Its presence is checked with a type
// assertion against TranscriptionModel.
type TranscriptionStreamer interface {
	DoStream(ctx context.Context, opts *TranscriptionStreamOptions) (*TranscriptionStreamResult, error)
}
