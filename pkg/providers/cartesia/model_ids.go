package cartesia

// Cartesia text-to-speech model IDs.
const (
	ModelSonic35     = "sonic-3.5"
	ModelSonic3      = "sonic-3"
	ModelSonic2      = "sonic-2"
	ModelSonicTurbo  = "sonic-turbo"
	ModelSonicLatest = "sonic-latest"
)

// Cartesia transcription model IDs. ModelInk2 (and any "ink-2-*" variant) is
// STREAMING-only (WebSocket-based) and is out of scope for this batch-only
// TranscriptionModel; DoTranscribe rejects it.
const (
	ModelInkWhisper = "ink-whisper"
	ModelInk2       = "ink-2"
)
