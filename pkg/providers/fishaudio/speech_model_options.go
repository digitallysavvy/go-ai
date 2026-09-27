package fishaudio

import "encoding/json"

// SpeechModelOptions contains Fish Audio-specific speech provider options,
// mirroring the TypeScript SDK's fishAudioSpeechModelOptionsSchema.
// See https://docs.fish.audio/api-reference/endpoint/openapi-v1/text-to-speech
type SpeechModelOptions struct {
	// ReferenceID selects the voice model. A single string selects one
	// speaker; a []string enables multi-speaker dialogue (S2-Pro models).
	// Takes precedence over the top-level Voice option.
	ReferenceID interface{} `json:"referenceId,omitempty"`

	// SampleRate is the output sample rate in Hz.
	SampleRate *int `json:"sampleRate,omitempty"`

	// Mp3Bitrate is the bitrate in kbps for mp3 output (64, 128, or 192).
	Mp3Bitrate *int `json:"mp3Bitrate,omitempty"`

	// OpusBitrate is the bitrate in bps for opus output, where -1000 selects
	// automatic (-1000, 24000, 32000, 48000, or 64000).
	OpusBitrate *int `json:"opusBitrate,omitempty"`

	// Latency is the latency/quality tradeoff: "low", "normal", or "balanced".
	Latency string `json:"latency,omitempty"`

	// Volume is the volume offset in dB.
	Volume *float64 `json:"volume,omitempty"`

	// NormalizeLoudness enables loudness normalization (S2 family only).
	NormalizeLoudness *bool `json:"normalizeLoudness,omitempty"`

	// Temperature governs expressiveness (0 to 1).
	Temperature *float64 `json:"temperature,omitempty"`

	// TopP controls diversity via nucleus sampling (0 to 1).
	TopP *float64 `json:"topP,omitempty"`

	// ChunkLength is the text segment size for processing (100 to 300).
	ChunkLength *int `json:"chunkLength,omitempty"`

	// MinChunkLength is the minimum characters before splitting into a new
	// chunk (0 to 100).
	MinChunkLength *int `json:"minChunkLength,omitempty"`

	// Normalize enables text normalization for English and Chinese.
	Normalize *bool `json:"normalize,omitempty"`

	// MaxNewTokens is the maximum audio tokens to generate per text chunk.
	MaxNewTokens *int `json:"maxNewTokens,omitempty"`

	// RepetitionPenalty discourages repeated audio patterns above 1.0.
	RepetitionPenalty *float64 `json:"repetitionPenalty,omitempty"`

	// ConditionOnPreviousChunks reuses prior audio as context for voice
	// consistency across chunks.
	ConditionOnPreviousChunks *bool `json:"conditionOnPreviousChunks,omitempty"`

	// EarlyStopThreshold is the early-stop threshold used in batch
	// processing (0 to 1).
	EarlyStopThreshold *float64 `json:"earlyStopThreshold,omitempty"`

	// Features are request-scoped flags passed through to the inference
	// backend, e.g. ["quality-guard"].
	Features []string `json:"features,omitempty"`
}

// TranscriptionModelOptions contains Fish Audio-specific transcription
// options, mirroring fishAudioTranscriptionModelOptionsSchema.
// See https://docs.fish.audio/api-reference/endpoint/openapi-v1/speech-to-text
type TranscriptionModelOptions struct {
	// Language is a hint only; Fish Audio's auto-detection is authoritative.
	Language string `json:"language,omitempty"`

	// IgnoreTimestamps skips precise timestamps when true. Fish Audio's API
	// default is true; this provider defaults it to false (see
	// buildTranscriptionForm) so that Segments is populated.
	IgnoreTimestamps *bool `json:"ignoreTimestamps,omitempty"`
}

// extractSpeechModelOptions decodes providerOptions["fishAudio"] into
// SpeechModelOptions, accepting either a native struct/pointer value (direct
// Go usage) or a generic map[string]interface{} (JSON-shaped input), by
// round-tripping through JSON — matching the camelCase field names used by
// the TypeScript SDK's zod schema.
func extractSpeechModelOptions(providerOptions map[string]interface{}) *SpeechModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["fishAudio"]
	if !ok || raw == nil {
		return nil
	}
	if opts, ok := raw.(SpeechModelOptions); ok {
		return &opts
	}
	if opts, ok := raw.(*SpeechModelOptions); ok {
		return opts
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var opts SpeechModelOptions
	if err := json.Unmarshal(b, &opts); err != nil {
		return nil
	}
	return &opts
}

// extractTranscriptionModelOptions decodes providerOptions["fishAudio"] into
// TranscriptionModelOptions, using the same dual-shape round trip as
// extractSpeechModelOptions.
func extractTranscriptionModelOptions(providerOptions map[string]interface{}) *TranscriptionModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["fishAudio"]
	if !ok || raw == nil {
		return nil
	}
	if opts, ok := raw.(TranscriptionModelOptions); ok {
		return &opts
	}
	if opts, ok := raw.(*TranscriptionModelOptions); ok {
		return opts
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var opts TranscriptionModelOptions
	if err := json.Unmarshal(b, &opts); err != nil {
		return nil
	}
	return &opts
}
