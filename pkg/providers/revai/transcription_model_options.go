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

// revaiConfigKeys are the schema keys forwarded to the job config (TS
// formDataConfig); unknown keys are stripped as zod strips them.
var revaiConfigKeys = []string{
	"metadata", "notification_config", "delete_after_seconds", "verbatim", "rush",
	"test_mode", "segments_to_transcribe", "speaker_names", "skip_diarization",
	"skip_postprocessing", "skip_punctuation", "remove_disfluencies",
	"remove_atmospherics", "filter_profanity", "speaker_channels_count",
	"speakers_count", "diarization_type", "custom_vocabulary_id",
	"custom_vocabularies", "strict_custom_vocabulary", "summarization_config",
	"translation_config", "language", "forced_alignment",
}

// extractOptions returns the job-config fields for providerOptions["revai"],
// mirroring TS parseProviderOptions + the formDataConfig `?? undefined`
// filtering. It returns nil only when the "revai" key itself is absent (TS
// skips zod validation, and so every `.default(...)`, then). Values are taken
// per field from the raw option map, so a fractional number or a null in one
// field never drops the others; the schema `.default(...)` values apply to
// absent (not null) fields, and null fields are omitted.
func extractOptions(providerOptions map[string]interface{}) map[string]interface{} {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["revai"]
	if !ok || raw == nil {
		return nil
	}

	var fields map[string]interface{}
	switch v := raw.(type) {
	case *TranscriptionModelOptions:
		if v == nil {
			return nil
		}
		raw = *v
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(b, &fields); err != nil {
		return nil
	}

	out := map[string]interface{}{}
	for _, k := range revaiConfigKeys {
		if v, present := fields[k]; present && v != nil {
			out[k] = v
		}
	}
	applyRevaiDefaults(fields, out)
	return out
}

// applyRevaiDefaults fills in the TypeScript SDK's zod `.default(...)`
// values for fields that are absent (an explicit null stays null and is then
// omitted), mirroring revaiTranscriptionModelOptionsSchema. Fields declared
// `.optional()` without a `.default()` are left untouched.
func applyRevaiDefaults(in, out map[string]interface{}) {
	def := func(m map[string]interface{}, src map[string]interface{}, k string, v interface{}) {
		if _, present := src[k]; !present {
			m[k] = v
		}
	}
	for _, k := range []string{"rush", "test_mode", "skip_diarization", "skip_postprocessing",
		"skip_punctuation", "remove_disfluencies", "remove_atmospherics", "filter_profanity", "forced_alignment"} {
		def(out, in, k, false)
	}
	def(out, in, "diarization_type", "standard")
	def(out, in, "language", "en")
	// Nested z.object schemas strip unknown keys and keep explicit nulls
	// (`.nullish()` fields), and `.default()` only fires for absent keys.
	pick := func(src map[string]interface{}, keys ...string) map[string]interface{} {
		cp := map[string]interface{}{}
		for _, k := range keys {
			if v, ok := src[k]; ok {
				cp[k] = v
			}
		}
		return cp
	}
	pickEach := func(v interface{}, keys ...string) interface{} {
		list, ok := v.([]interface{})
		if !ok {
			return v
		}
		out := make([]interface{}, len(list))
		for i, item := range list {
			if m, ok := item.(map[string]interface{}); ok {
				out[i] = pick(m, keys...)
			} else {
				out[i] = item
			}
		}
		return out
	}
	if nc, ok := out["notification_config"].(map[string]interface{}); ok {
		cp := pick(nc, "url", "auth_headers")
		if ah, ok := cp["auth_headers"].(map[string]interface{}); ok {
			cp["auth_headers"] = pick(ah, "Authorization")
		}
		out["notification_config"] = cp
	}
	if v, ok := out["segments_to_transcribe"]; ok {
		out["segments_to_transcribe"] = pickEach(v, "start", "end")
	}
	if v, ok := out["speaker_names"]; ok {
		out["speaker_names"] = pickEach(v, "display_name")
	}
	if v, ok := out["custom_vocabularies"]; ok {
		out["custom_vocabularies"] = pickEach(v) // z.array(z.object({})) strips every key
	}
	if sc, ok := out["summarization_config"].(map[string]interface{}); ok {
		cp := pick(sc, "model", "type", "prompt")
		def(cp, sc, "model", "standard")
		def(cp, sc, "type", "paragraph")
		out["summarization_config"] = cp
	}
	if tc, ok := out["translation_config"].(map[string]interface{}); ok {
		cp := pick(tc, "target_languages", "model")
		if v, ok := cp["target_languages"]; ok {
			cp["target_languages"] = pickEach(v, "language")
		}
		def(cp, tc, "model", "standard")
		out["translation_config"] = cp
	}
}
