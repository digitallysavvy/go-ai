package elevenlabs

import "encoding/json"

// TranscriptionModelOptions contains ElevenLabs-specific batch transcription
// options, mirroring the TypeScript SDK's
// elevenLabsTranscriptionModelOptionsSchema (batch-relevant fields only; the
// `streaming` sub-object is realtime-only and out of scope here).
// See https://elevenlabs.io/docs/api-reference/speech-to-text/convert
type TranscriptionModelOptions struct {
	// LanguageCode is an ISO 639 language hint for the audio.
	LanguageCode string `json:"languageCode,omitempty"`

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
