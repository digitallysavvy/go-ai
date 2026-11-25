package cartesia

import "encoding/json"

// SpeechModelOptions contains Cartesia-specific speech provider options,
// mirroring the TypeScript SDK's cartesiaSpeechModelOptionsSchema.
// See https://docs.cartesia.ai/api-reference/tts/bytes
type SpeechModelOptions struct {
	// Container is the output audio container: "raw", "wav", or "mp3".
	Container string `json:"container,omitempty"`

	// Encoding is the audio encoding: "pcm_f32le", "pcm_s16le", "pcm_mulaw",
	// or "pcm_alaw".
	Encoding string `json:"encoding,omitempty"`

	// SampleRate is the output sample rate in Hz (8000, 16000, 22050, 24000,
	// 44100, or 48000).
	SampleRate *int `json:"sampleRate,omitempty"`

	// BitRate is the bitrate for mp3 output in bits per second (32000,
	// 64000, 96000, 128000, or 192000).
	BitRate *int `json:"bitRate,omitempty"`

	// Speed controls the speed of the generated speech (0.6 to 1.5).
	Speed *float64 `json:"speed,omitempty"`

	// Language is the ISO 639-1 language code to generate speech in.
	Language string `json:"language,omitempty"`

	// Volume of the generated speech, between 0.5 and 2 (Sonic 3 and
	// newer); sent as generation_config.volume.
	Volume *float64 `json:"volume,omitempty"`

	// Emotion of the generated speech (beta, best with English; Sonic 3 and
	// newer), e.g. "neutral", "happy", "calm"; sent as
	// generation_config.emotion.
	Emotion string `json:"emotion,omitempty"`

	// Locale is the regional locale code to generate speech in (e.g.
	// "en-GB", "es-MX"; Sonic 3.6 and later). It cannot be combined with
	// Language.
	Locale string `json:"locale,omitempty"`

	// Normalization controls text normalization ("auto", "off", or a
	// language or locale code).
	Normalization string `json:"normalization,omitempty"`

	// PronunciationDictionaryID is the ID of a pronunciation dictionary
	// created with the Cartesia API (Sonic 3 and newer); sent as
	// pronunciation_dict_id.
	PronunciationDictionaryID string `json:"pronunciationDictionaryId,omitempty"`
}

// extractSpeechModelOptions decodes providerOptions["cartesia"] into
// SpeechModelOptions, accepting either a native struct/pointer value or a
// generic map[string]interface{} by round-tripping through JSON.
func extractSpeechModelOptions(providerOptions map[string]interface{}) *SpeechModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["cartesia"]
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
