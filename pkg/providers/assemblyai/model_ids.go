package assemblyai

// AssemblyAI transcription model IDs.
//
// "best" is the legacy default model, selected via the deprecated singular
// `speech_model` request parameter. All other models are only reachable via
// the `speech_models` array parameter.
// See https://www.assemblyai.com/docs/pre-recorded-audio/select-the-speech-model
const (
	// ModelBest is the legacy default AssemblyAI model.
	ModelBest = "best"

	// ModelUniversal2 is an earlier Universal model generation.
	ModelUniversal2 = "universal-2"

	// ModelUniversal3Pro is being replaced by ModelUniversal35Pro.
	ModelUniversal3Pro = "universal-3-pro"

	// ModelUniversal35Pro is AssemblyAI's latest flagship model.
	ModelUniversal35Pro = "universal-3-5-pro"
)
