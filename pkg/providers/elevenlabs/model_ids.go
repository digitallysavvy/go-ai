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
