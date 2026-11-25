package google

const (
	ModelGemini25FlashTTS        = "gemini-2.5-flash-preview-tts"
	ModelGemini25ProTTS          = "gemini-2.5-pro-preview-tts"
	ModelGemini31FlashTTSPreview = "gemini-3.1-flash-tts-preview"
	ModelGemini38FlashTTS        = "gemini-3.8-flash-tts"
	ModelGemini38FlashLiteTTS    = "gemini-3.8-flash-lite-tts"
)

// GoogleSpeechMetadata holds turn-level directions for Gemini 3.8 TTS.
type GoogleSpeechMetadata struct {
	// Speaker names the configured speaker for a turn (multi-speaker audio).
	Speaker string `json:"speaker,omitempty"`
	// Style is a free-form delivery direction. A non-nil empty style still
	// overrides the call's instructions (TS `style ?? instructions`).
	Style *string `json:"style,omitempty"`
}

// GoogleSpeechTurn is one turn of a structured transcript.
type GoogleSpeechTurn struct {
	Text           string                `json:"text"`
	SpeechMetadata *GoogleSpeechMetadata `json:"speechMetadata,omitempty"`
}

// GoogleSpeechModelOptions contains Google Gemini TTS provider options.
type GoogleSpeechModelOptions struct {
	// SpeechMetadata is turn-level direction for the top-level text (Gemini 3.8 TTS).
	SpeechMetadata *GoogleSpeechMetadata `json:"speechMetadata,omitempty"`

	// Turns is a structured transcript that replaces the top-level text
	// (Gemini 3.8 TTS). Pass an empty Text when using turns; each multi-speaker
	// turn must name a configured speaker in SpeechMetadata.
	Turns []GoogleSpeechTurn `json:"turns,omitempty"`

	// MultiSpeakerVoiceConfig configures Gemini multi-speaker TTS. When set it
	// takes precedence over the top-level voice.
	MultiSpeakerVoiceConfig map[string]interface{} `json:"multiSpeakerVoiceConfig,omitempty"`
}
