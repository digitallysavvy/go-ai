package cartesia

import "encoding/json"

// TranscriptionModelOptions contains Cartesia-specific transcription
// options, mirroring cartesiaTranscriptionModelOptionsSchema.
// See https://docs.cartesia.ai/api-reference/stt/transcribe
type TranscriptionModelOptions struct {
	// Language is the ISO 639-1 language code of the audio. Defaults to
	// English.
	Language string `json:"language,omitempty"`

	// TimestampGranularities are the timestamp granularities to populate.
	// Currently only "word" is supported.
	TimestampGranularities []string `json:"timestampGranularities,omitempty"`

	// Streaming holds options for realtime Ink 2 transcription over
	// WebSocket, consumed by TranscriptionModel.DoStream. For the
	// batch-only DoTranscribe path its mere presence triggers the
	// "unsupported" warning below, matching the TypeScript SDK's doGenerate
	// behavior.
	Streaming *StreamingOptions `json:"streaming,omitempty"`
}

// StreamingOptions contains realtime Ink 2 (WebSocket) transcription
// options, mirroring cartesiaTranscriptionModelOptionsSchema.streaming.
type StreamingOptions struct {
	// Encoding is the raw audio encoding sent to Ink 2. Defaults to the
	// encoding inferred from inputAudioFormat.type.
	Encoding string `json:"encoding,omitempty"`

	// TurnDetection uses Cartesia's native turn detection endpoint.
	// Defaults to true. Set to false to finalize the transcript only when
	// the audio stream ends.
	TurnDetection *bool `json:"turnDetection,omitempty"`

	// Keyterms are words or phrases the model should favor, such as product
	// names or jargon (up to 100 terms and 1200 characters in total).
	Keyterms []string `json:"keyterms,omitempty"`

	// TurnStartThreshold starts the turn when the likelihood that the user
	// is speaking rises above it. Ignored (with a warning) when
	// TurnDetection is false.
	TurnStartThreshold *float64 `json:"turnStartThreshold,omitempty"`

	// TurnEagerEndThreshold signals an early end of turn when the
	// likelihood that the user is speaking falls below it. Ignored (with a
	// warning) when TurnDetection is false.
	TurnEagerEndThreshold *float64 `json:"turnEagerEndThreshold,omitempty"`

	// TurnEndThreshold ends the turn when the likelihood that the user is
	// speaking falls below it. Ignored (with a warning) when TurnDetection
	// is false.
	TurnEndThreshold *float64 `json:"turnEndThreshold,omitempty"`

	// TurnEndTimeoutMs is the longest wait in milliseconds after the user
	// stops speaking before the turn ends. Ignored (with a warning) when
	// TurnDetection is false.
	TurnEndTimeoutMs *int64 `json:"turnEndTimeoutMs,omitempty"`
}

// extractTranscriptionModelOptions decodes providerOptions["cartesia"] into
// TranscriptionModelOptions, accepting either a native struct/pointer value
// or a generic map[string]interface{} by round-tripping through JSON.
func extractTranscriptionModelOptions(providerOptions map[string]interface{}) *TranscriptionModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["cartesia"]
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
