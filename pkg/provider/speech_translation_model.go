package provider

import (
	"context"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Speech translation stream part types, mirroring TS
// Experimental_SpeechTranslationModelV4StreamPart's discriminated `type`
// field. Speech translation is a streaming-only modality: models translate
// live source audio into target-language audio and text.
const (
	SpeechTranslationStreamPartTypeStreamStart             = "stream-start"
	SpeechTranslationStreamPartTypeAudio                   = "audio"
	SpeechTranslationStreamPartTypeOutputTextDelta         = "output-text-delta"
	SpeechTranslationStreamPartTypeOutputTextFinal         = "output-text-final"
	SpeechTranslationStreamPartTypeSourceTranscriptDelta   = "source-transcript-delta"
	SpeechTranslationStreamPartTypeSourceTranscriptPartial = "source-transcript-partial"
	SpeechTranslationStreamPartTypeSourceTranscriptFinal   = "source-transcript-final"
	SpeechTranslationStreamPartTypeResponseMetadata        = "response-metadata"
	SpeechTranslationStreamPartTypeFinish                  = "finish"
	SpeechTranslationStreamPartTypeRaw                     = "raw"
	SpeechTranslationStreamPartTypeError                   = "error"
)

// SpeechTranslationUsage is usage information for a speech translation call.
// All fields are optional because providers report usage with different
// granularity (TS Experimental_SpeechTranslationModelV4Usage).
type SpeechTranslationUsage struct {
	InputAudioSeconds *float64
	InputAudioTokens  *int
	OutputAudioTokens *int
	InputTextTokens   *int
	OutputTextTokens  *int
}

// SpeechTranslationStreamPart is one part of a speech translation model's
// stream, mirroring TS Experimental_SpeechTranslationModelV4StreamPart.
// Fields are grouped by which Type they apply to.
type SpeechTranslationStreamPart struct {
	Type string

	// stream-start
	Warnings []types.Warning

	// audio
	ID        string
	AudioData []byte

	// output-text-delta / output-text-final / source-transcript-* /
	// audio's providerMetadata
	Delta            string
	Text             string
	StartSecond      *float64
	EndSecond        *float64
	ChannelIndex     *int
	ProviderMetadata map[string]interface{}

	// response-metadata
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}

	// finish
	SourceText        string
	OutputText        string
	DurationInSeconds *float64
	Usage             *SpeechTranslationUsage

	// raw
	RawValue interface{}

	// error
	Err interface{}
}

// SpeechTranslationStream is a Next()-based stream of
// SpeechTranslationStreamPart, following the same convention as
// provider.TextStream.
type SpeechTranslationStream interface {
	Next() (*SpeechTranslationStreamPart, error)
	Err() error
	Close() error
}

// SpeechTranslationStreamOptions contains options for
// SpeechTranslationModel.DoStream (TS
// Experimental_SpeechTranslationModelV4StreamOptions).
type SpeechTranslationStreamOptions struct {
	// Audio is the source audio chunks to transform.
	Audio AudioStream

	// InputAudioFormat is the input audio format for the raw audio chunks.
	InputAudioFormat AudioFormat

	// TargetLanguage is the language to produce output audio and text in, as
	// a BCP-47-style language tag (e.g. "en", "es", "fr-CA").
	TargetLanguage string

	// SourceLanguage is the language of the source audio, as a BCP-47-style
	// language tag. When empty, providers should auto-detect the source
	// language.
	SourceLanguage string

	// OutputAudioFormat is the desired audio format for output audio chunks.
	// When nil, the provider default output format is used.
	OutputAudioFormat *AudioFormat

	// ProviderOptions contains provider-specific request options.
	ProviderOptions map[string]interface{}

	// AbortSignal for cancellation.
	AbortSignal context.Context

	// Additional HTTP/WebSocket headers.
	Headers map[string]string

	// IncludeRawChunks requests provider raw chunks in the stream.
	IncludeRawChunks bool
}

// SpeechTranslationStreamResult is the result of a
// SpeechTranslationModel.DoStream call (TS
// Experimental_SpeechTranslationModelV4StreamResult).
type SpeechTranslationStreamResult struct {
	Stream      SpeechTranslationStream
	RequestBody interface{}
	Response    *TranscriptionStreamResponseMetadata
}

// SpeechTranslationModel is the v4 specification for speech translation
// models (TS Experimental_SpeechTranslationModelV4). Speech translation is a
// streaming-only modality, so the only generation method is DoStream.
type SpeechTranslationModel interface {
	SpecificationVersion() string
	Provider() string
	ModelID() string

	DoStream(ctx context.Context, opts *SpeechTranslationStreamOptions) (*SpeechTranslationStreamResult, error)
}
