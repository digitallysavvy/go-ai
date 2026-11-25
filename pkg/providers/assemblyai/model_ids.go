package assemblyai

// AssemblyAI model IDs (TS assemblyaiTranscriptionModelIds and
// assemblyaiSpeechModelIds, assemblyai-model-catalog.ts). TranscriptionModel
// and SpeechModel accept any other string too, which is sent through
// `speech_models` / `model_id` unchanged; the API rejects IDs it no longer
// accepts (best, slam-1, universal-3-pro).
// See https://www.assemblyai.com/docs/pre-recorded-audio/select-the-speech-model
const (
	// ModelAssemblyAISTT37 is AssemblyAI's flagship pre-recorded speech-to-text
	// model.
	ModelAssemblyAISTT37 = "assemblyai-stt-3-7"

	// ModelUniversal2 is an earlier Universal model generation; it remains
	// available for languages ModelAssemblyAISTT37 does not support.
	ModelUniversal2 = "universal-2"

	// ModelUniversal35Pro is deprecated in favor of ModelAssemblyAISTT37.
	ModelUniversal35Pro = "universal-3-5-pro"

	// SpeechModelTTS1 is AssemblyAI's text-to-speech model.
	SpeechModelTTS1 = "assemblyai-tts-1"
)
