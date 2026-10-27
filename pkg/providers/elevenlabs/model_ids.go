package elevenlabs

// ElevenLabs transcription model IDs.
const (
	// ModelScribeV1 is ElevenLabs' first-generation batch transcription model.
	ModelScribeV1 = "scribe_v1"

	// ModelScribeV2 is ElevenLabs' second-generation batch transcription model.
	ModelScribeV2 = "scribe_v2"

	// ModelScribeV2Realtime is a STREAMING-only realtime transcription model
	// (WebSocket-based, TranscriptionModel.DoStream). DoTranscribe rejects it.
	ModelScribeV2Realtime = "scribe_v2_realtime"
)

// ElevenLabs speech model IDs (TS ElevenLabsSpeechModelId). Any string is
// accepted; these are the known ones.
const (
	// SpeechModelV4 and SpeechModelV4Turbo are the v4 family. Streaming them
	// uses the Text-to-Dialogue WebSocket (providerOptions dialogue).
	SpeechModelV4      = "eleven_v4"
	SpeechModelV4Turbo = "eleven_v4_turbo"

	// SpeechModelV3 and SpeechModelV3Conversational are the v3 family.
	// Streaming them uses the Text-to-Dialogue WebSocket, which takes the
	// whole script in one message.
	SpeechModelV3               = "eleven_v3"
	SpeechModelV3Conversational = "eleven_v3_conversational"

	SpeechModelMultilingualV2 = "eleven_multilingual_v2"
	SpeechModelFlashV25       = "eleven_flash_v2_5"
	SpeechModelFlashV2        = "eleven_flash_v2"
	SpeechModelTurboV25       = "eleven_turbo_v2_5"
	SpeechModelTurboV2        = "eleven_turbo_v2"
	SpeechModelMonolingualV1  = "eleven_monolingual_v1"
	SpeechModelMultilingualV1 = "eleven_multilingual_v1"
)
