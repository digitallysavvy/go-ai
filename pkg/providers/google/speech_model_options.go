package google

const (
	ModelGemini25FlashTTS        = "gemini-2.5-flash-preview-tts"
	ModelGemini25ProTTS          = "gemini-2.5-pro-preview-tts"
	ModelGemini31FlashTTSPreview = "gemini-3.1-flash-tts-preview"
)

// GoogleSpeechModelOptions contains Google Gemini TTS provider options.
type GoogleSpeechModelOptions struct {
	// MultiSpeakerVoiceConfig configures Gemini multi-speaker TTS. When set it
	// takes precedence over the top-level voice.
	MultiSpeakerVoiceConfig map[string]interface{} `json:"multiSpeakerVoiceConfig,omitempty"`
}
