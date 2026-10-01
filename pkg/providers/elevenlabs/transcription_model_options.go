package elevenlabs

import "encoding/json"

// TranscriptionModelOptions contains ElevenLabs-specific transcription
// options, mirroring the TypeScript SDK's
// elevenLabsTranscriptionModelOptionsSchema: batch-relevant fields plus the
// `streaming` sub-object consumed by scribe_v2_realtime's DoStream.
// See https://elevenlabs.io/docs/api-reference/speech-to-text/convert
type TranscriptionModelOptions struct {
	// LanguageCode is an ISO 639 language hint for the audio. A pointer
	// distinguishes "not set" (nil) from an explicit empty string, mirroring
	// TS's `languageCode: z.string().nullish()` (no default): TS forwards an
	// explicit "" to the wire (it is only `??`-coalesced away when null or
	// undefined), which a plain Go string cannot represent since its zero
	// value is indistinguishable from "unset".
	LanguageCode *string `json:"languageCode,omitempty"`

	// TagAudioEvents tags non-speech audio events. Defaults to true.
	TagAudioEvents *bool `json:"tagAudioEvents,omitempty"`

	// NumSpeakers hints the expected number of speakers (1-32).
	NumSpeakers *int `json:"numSpeakers,omitempty"`

	// TimestampsGranularity is "none", "word", or "character". Defaults to "word".
	TimestampsGranularity string `json:"timestampsGranularity,omitempty"`

	// Diarize enables speaker diarization. Defaults to false.
	Diarize *bool `json:"diarize,omitempty"`

	// FileFormat is "pcm_s16le_16" or "other". Defaults to "other".
	FileFormat string `json:"fileFormat,omitempty"`

	// Streaming holds scribe_v2_realtime-only options; nil for batch calls
	// that never set providerOptions.elevenlabs.streaming.
	Streaming *StreamingOptions `json:"streaming,omitempty"`
}

// StreamingOptions contains scribe_v2_realtime-only options, mirroring the
// TypeScript SDK's elevenLabsTranscriptionModelOptionsSchema.streaming
// sub-schema.
// See https://elevenlabs.io/docs/api-reference/speech-to-text/v-1-speech-to-text-realtime
type StreamingOptions struct {
	// CommitStrategy is "manual" or "vad".
	CommitStrategy string `json:"commitStrategy,omitempty"`

	// EnableLogging opts the session in/out of ElevenLabs server-side logging.
	EnableLogging *bool `json:"enableLogging,omitempty"`

	// FilterBackgroundAudio filters non-speech audio server-side. Cannot be
	// combined with IncludeTimestamps or IncludeLanguageDetection.
	FilterBackgroundAudio *bool `json:"filterBackgroundAudio,omitempty"`

	// IncludeLanguageDetection requests detected-language metadata.
	IncludeLanguageDetection *bool `json:"includeLanguageDetection,omitempty"`

	// IncludeTimestamps requests per-word timestamps on final transcripts.
	IncludeTimestamps *bool `json:"includeTimestamps,omitempty"`

	// Keyterms are up to 50 terms (each <=20 chars) to bias recognition toward.
	Keyterms []string `json:"keyterms,omitempty"`

	// MinSilenceDurationMs is the minimum silence duration (50-2000ms) for VAD.
	MinSilenceDurationMs *int `json:"minSilenceDurationMs,omitempty"`

	// MinSpeechDurationMs is the minimum speech duration (50-2000ms) for VAD.
	MinSpeechDurationMs *int `json:"minSpeechDurationMs,omitempty"`

	// NoVerbatim disables verbatim transcription (fillers, false starts).
	NoVerbatim *bool `json:"noVerbatim,omitempty"`

	// PreviousText seeds the model with prior conversational context on the
	// first audio chunk. A pointer distinguishes "not set" (nil) from an
	// explicit empty string, mirroring TS's `previousText: z.string().nullish()`:
	// TS sends `previous_text` on the first chunk whenever the option is
	// non-null (`previousText != null`), including an explicit "", which a
	// plain Go string cannot represent.
	PreviousText *string `json:"previousText,omitempty"`

	// SecondaryLanguages are additional language codes the session may switch
	// between.
	SecondaryLanguages []string `json:"secondaryLanguages,omitempty"`

	// VadSilenceThresholdSecs is the VAD silence threshold (0.3-3s).
	VadSilenceThresholdSecs *float64 `json:"vadSilenceThresholdSecs,omitempty"`

	// VadThreshold is the VAD speech-detection threshold (0.1-0.9).
	VadThreshold *float64 `json:"vadThreshold,omitempty"`
}

// extractTranscriptionOptions reads ElevenLabs-specific batch transcription
// options from the provider options map, applying the same defaults the TS
// SDK's zod schema applies once the "elevenlabs" key is present at all. It
// returns (nil, false) when providerOptions has no "elevenlabs" key, mirroring
// parseProviderOptions returning undefined in that case.
func extractTranscriptionOptions(providerOptions map[string]interface{}) (opts *TranscriptionModelOptions, present bool, hasStreaming bool) {
	if providerOptions == nil {
		return nil, false, false
	}
	raw, ok := providerOptions["elevenlabs"]
	if !ok {
		return nil, false, false
	}

	opts = &TranscriptionModelOptions{}
	if rawMap, ok := raw.(map[string]interface{}); ok {
		if v, ok := rawMap["streaming"]; ok && v != nil {
			hasStreaming = true
		}
	}
	if raw != nil {
		if b, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(b, opts)
		}
	}

	// Apply schema defaults (zod .default()), matching TS parseProviderOptions.
	if opts.TagAudioEvents == nil {
		tagDefault := true
		opts.TagAudioEvents = &tagDefault
	}
	if opts.TimestampsGranularity == "" {
		opts.TimestampsGranularity = "word"
	}
	if opts.Diarize == nil {
		diarizeDefault := false
		opts.Diarize = &diarizeDefault
	}
	if opts.FileFormat == "" {
		opts.FileFormat = "other"
	}

	return opts, true, hasStreaming
}
