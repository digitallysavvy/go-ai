package fishaudio

// Fish Audio TTS model IDs, sent via the `model` HTTP header.
//
// ModelS21Pro is the model Fish Audio recommends by default. ModelS21ProFree
// is a free developer tier with no time-to-first-audio or data-processing
// guarantees, so prefer ModelS21Pro for production use.
//
// https://docs.fish.audio/api-reference/endpoint/openapi-v1/text-to-speech
const (
	ModelS1          = "s1"
	ModelS2Pro       = "s2-pro"
	ModelS21Pro      = "s2.1-pro"
	ModelS21ProFree  = "s2.1-pro-free"
	ModelTranscribe1 = "transcribe-1"
)
