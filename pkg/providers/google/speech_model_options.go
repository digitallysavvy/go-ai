package google

const (
	ModelGemini25FlashTTS        = "gemini-2.5-flash-preview-tts"
	ModelGemini25ProTTS          = "gemini-2.5-pro-preview-tts"
	ModelGemini31FlashTTSPreview = "gemini-3.1-flash-tts-preview"
	ModelGemini38FlashTTS        = "gemini-3.8-flash-tts"
	ModelGemini38FlashLiteTTS    = "gemini-3.8-flash-lite-tts"
)

// GoogleSpeechModelOptions contains Google Gemini TTS provider options.
type GoogleSpeechModelOptions struct {
	// MultiSpeakerVoiceConfig configures Gemini multi-speaker TTS. When set it
	// takes precedence over the top-level voice.
	MultiSpeakerVoiceConfig map[string]interface{} `json:"multiSpeakerVoiceConfig,omitempty"`
}
