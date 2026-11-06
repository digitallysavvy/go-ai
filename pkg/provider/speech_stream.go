package provider

import (
	"context"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Experimental streaming speech model spec (TS
// Experimental_StreamingSpeechModelV4 and friends).
//
// Streaming speech is an additive, optional capability: a SpeechModel that
// also implements SpeechStreamer can synthesize speech for streamed text. The
// stable SpeechModel interface is unchanged. Check for support with a type
// assertion:
//
//	if s, ok := model.(provider.SpeechStreamer); ok { ... }
//
// The spec is experimental and may change in patch releases.

// Speech stream part types, mirroring the discriminated `type` field of TS
// Experimental_SpeechModelV4StreamPart.
const (
	SpeechStreamPartTypeStreamStart      = "stream-start"
	SpeechStreamPartTypeAudio            = "audio"
	SpeechStreamPartTypeResponseMetadata = "response-metadata"
	SpeechStreamPartTypeFinish           = "finish"
	SpeechStreamPartTypeRaw              = "raw"
	SpeechStreamPartTypeError            = "error"
)

// SpeechStreamTextPart is one piece of streamed input text (TS
// Experimental_SpeechModelV4StreamTextPart). Consecutive parts are
// synthesized in order as one continuous script.
type SpeechStreamTextPart struct {
	// Text to synthesize. Parts may split anywhere, including mid-word.
	Text string

	// Voice that speaks this part. Like the call's voice it is
	// provider-specific. It defaults to the call's voice (or the provider's
	// default voice). Providers that support several voices per stream (for
	// example dialogue) switch speakers when it changes. Single-voice
	// providers fail the stream when a part's voice differs from the call's.
	Voice string

	// ProviderOptions are provider-specific options for this part, keyed by
	// provider name (for example to start a new speaker turn without
	// changing the voice).
	ProviderOptions map[string]interface{}
}

// SpeechTextStream is a producer-controlled source of streamed text parts. It
// mirrors the semantics of a TS ReadableStream<SpeechModelV4StreamTextPart>:
// Next returns parts until io.EOF, and Cancel tells the producer that
// consumption failed or was abandoned.
//
// The provider owns the stream once DoStreamSpeech is called. It must call
// Cancel on every terminal path other than a normal finish: when the context
// is cancelled, when the consumer closes the returned stream, when setup
// fails, and when the provider fails. Cancel is idempotent and reason may be
// nil.
type SpeechTextStream interface {
	// Next returns the next text part, or io.EOF when the producer has no
	// more text. It honors ctx cancellation.
	Next(ctx context.Context) (*SpeechStreamTextPart, error)

	// Cancel notifies the producer that consumption failed or was abandoned.
	Cancel(reason error)
}

// SpeechStreamUsage is usage information for a speech stream call (TS
// Experimental_SpeechModelV4StreamUsage). All fields are optional because
// providers bill speech in different units.
type SpeechStreamUsage struct {
	// InputCharacters is the number of input text characters (Unicode code
	// points) sent for synthesis, if known.
	InputCharacters *int `json:"inputCharacters,omitempty"`

	// InputTokens is the number of input text tokens, if reported.
	InputTokens *int `json:"inputTokens,omitempty"`

	// OutputTokens is the number of output audio tokens, if reported.
	OutputTokens *int `json:"outputTokens,omitempty"`

	// Raw is the provider's raw usage information.
	Raw map[string]interface{} `json:"raw,omitempty"`
}

// SpeechStreamPart is one part of a speech model's stream, mirroring TS
// Experimental_SpeechModelV4StreamPart. Fields are grouped by Type.
type SpeechStreamPart struct {
	Type string

	// stream-start
	Warnings []types.Warning

	// audio: Audio holds raw audio bytes (a TS base64 string is decoded by
	// the producer). A chunk may be empty when it only carries
	// ProviderMetadata for a point in the audio, such as an end-of-turn
	// marker.
	ID               string
	Audio            []byte
	AudioFormat      AudioFormat
	ProviderMetadata map[string]interface{} // audio, finish

	// response-metadata
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}

	// finish
	Usage *SpeechStreamUsage

	// raw
	RawValue interface{}

	// error: a non-fatal error; the stream continues. Providers signal an
	// unrecoverable failure by failing the stream (Next returns an error) or
	// by ending it without a finish part.
	Err interface{}
}

// SpeechStream is a Next()-based stream of SpeechStreamPart, following the
// same convention as TextStream: Next returns io.EOF when the stream is
// complete, Err reports any error, and Close releases resources (safe to
// call multiple times).
type SpeechStream interface {
	Next() (*SpeechStreamPart, error)
	Err() error
	Close() error
}

// SpeechStreamOptions contains options for SpeechStreamer.DoStreamSpeech (TS
// Experimental_SpeechModelV4StreamOptions).
type SpeechStreamOptions struct {
	// Text is the streamed input text. The end of the stream signals the end
	// of the input.
	Text SpeechTextStream

	// Voice to use for speech synthesis (provider-specific).
	Voice string

	// OutputFormat is the desired format of the output audio chunks, e.g.
	// {Type: "audio/pcm", Rate: 24000}. Nil uses the provider default.
	OutputFormat *AudioFormat

	// Instructions for the speech generation.
	Instructions string

	// Speed of the speech generation.
	Speed *float64

	// Language is an ISO 639-1 code or "auto". Provider support varies.
	Language string

	// ProviderOptions contains provider-specific options.
	ProviderOptions map[string]interface{}

	// AbortSignal for cancellation.
	AbortSignal context.Context

	// Headers are additional HTTP/WebSocket headers.
	Headers map[string]string

	// IncludeRawChunks requests provider raw chunks in the stream.
	IncludeRawChunks bool
}

// SpeechStreamResponseMetadata is optional response data for a speech stream.
type SpeechStreamResponseMetadata struct {
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}
}

// SpeechStreamResult is the result of SpeechStreamer.DoStreamSpeech (TS
// Experimental_SpeechModelV4StreamResult).
type SpeechStreamResult struct {
	// Stream is the speech stream.
	Stream SpeechStream

	// RequestBody is the request body or setup payload sent to the provider,
	// for telemetry and debugging.
	RequestBody interface{}

	// Response is optional response data.
	Response *SpeechStreamResponseMetadata
}

// SpeechStreamer is an optional capability implemented by speech models that
// can stream speech audio for streamed text input (TS
// Experimental_StreamingSpeechModelV4.doStream).
type SpeechStreamer interface {
	DoStreamSpeech(ctx context.Context, opts *SpeechStreamOptions) (*SpeechStreamResult, error)
}
