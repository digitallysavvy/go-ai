package revai

import "encoding/json"

// TranscriptionModelOptions contains Rev.ai-specific transcription options,
// mirroring the TypeScript SDK's revaiTranscriptionModelOptionsSchema. Field
// names intentionally match Rev.ai's own snake_case job submission API
// (matching the TypeScript SDK, which uses the same names), not the SDK's
// usual camelCase provider-option convention.
// See https://docs.rev.ai/api/asynchronous/reference/#operation/SubmitTranscriptionJob
type TranscriptionModelOptions struct {
	Metadata               string                      `json:"metadata,omitempty"`
	NotificationConfig     *NotificationConfig         `json:"notification_config,omitempty"`
	DeleteAfterSeconds     *int                        `json:"delete_after_seconds,omitempty"`
	Verbatim               *bool                       `json:"verbatim,omitempty"`
	Rush                   *bool                       `json:"rush,omitempty"`
	TestMode               *bool                       `json:"test_mode,omitempty"`
	SegmentsToTranscribe   []TranscriptionSegmentRange `json:"segments_to_transcribe,omitempty"`
	SpeakerNames           []SpeakerName               `json:"speaker_names,omitempty"`
	SkipDiarization        *bool                       `json:"skip_diarization,omitempty"`
	SkipPostprocessing     *bool                       `json:"skip_postprocessing,omitempty"`
	SkipPunctuation        *bool                       `json:"skip_punctuation,omitempty"`
	RemoveDisfluencies     *bool                       `json:"remove_disfluencies,omitempty"`
	RemoveAtmospherics     *bool                       `json:"remove_atmospherics,omitempty"`
	FilterProfanity        *bool                       `json:"filter_profanity,omitempty"`
	SpeakerChannelsCount   *int                        `json:"speaker_channels_count,omitempty"`
	SpeakersCount          *int                        `json:"speakers_count,omitempty"`
	DiarizationType        string                      `json:"diarization_type,omitempty"`
	CustomVocabularyID     string                      `json:"custom_vocabulary_id,omitempty"`
	CustomVocabularies     []map[string]interface{}    `json:"custom_vocabularies,omitempty"`
	StrictCustomVocabulary *bool                       `json:"strict_custom_vocabulary,omitempty"`
	SummarizationConfig    *SummarizationConfig        `json:"summarization_config,omitempty"`
	TranslationConfig      *TranslationConfig          `json:"translation_config,omitempty"`
	Language               string                      `json:"language,omitempty"`
	ForcedAlignment        *bool                       `json:"forced_alignment,omitempty"`
}

// NotificationConfig configures a webhook to invoke when processing is complete.
type NotificationConfig struct {
	URL         string       `json:"url"`
	AuthHeaders *AuthHeaders `json:"auth_headers,omitempty"`
}

// AuthHeaders carries the single supported callback authorization header.
type AuthHeaders struct {
	Authorization string `json:"Authorization"`
}

// TranscriptionSegmentRange specifies a portion of the audio to transcribe.
type TranscriptionSegmentRange struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// SpeakerName assigns a display name to a speaker in the transcript.
type SpeakerName struct {
	DisplayName string `json:"display_name"`
}

// SummarizationConfig configures transcript summarization.
type SummarizationConfig struct {
	Model  string `json:"model,omitempty"`
	Type   string `json:"type,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

// TranslationConfig configures transcript translation.
type TranslationConfig struct {
	TargetLanguages []TranslationTarget `json:"target_languages"`
	Model           string              `json:"model,omitempty"`
}

// TranslationTarget is one target language for translation.
type TranslationTarget struct {
	Language string `json:"language"`
}

// extractOptions decodes providerOptions["revai"] into
// TranscriptionModelOptions, accepting either a native struct/pointer value
// or a generic map[string]interface{} by round-tripping through JSON.
func extractOptions(providerOptions map[string]interface{}) *TranscriptionModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["revai"]
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
