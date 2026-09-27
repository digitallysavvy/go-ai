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
	// WebSocket. It is out of scope for this batch-only TranscriptionModel;
	// its presence only triggers the "unsupported" warning below, matching
	// the TypeScript SDK's doGenerate behavior.
	Streaming map[string]interface{} `json:"streaming,omitempty"`
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
