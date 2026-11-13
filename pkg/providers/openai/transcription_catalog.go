package openai

import "strings"

// transcriptionSupport is a TS ModelCapability: true, false or "unknown".
type transcriptionSupport int

const (
	transcriptionSupportUnknown transcriptionSupport = iota
	transcriptionSupportYes
	transcriptionSupportNo
)

// transcriptionModelEntry is the Go form of a TS
// OpenAITranscriptionModelEntry (openai-model-catalog.ts at 30548ee7f9):
// which endpoints a transcription model supports and the request defaults
// it needs.
type transcriptionModelEntry struct {
	// rest is capabilities.transcription.transcription (REST
	// /audio/transcriptions).
	rest transcriptionSupport
	// streaming is capabilities.transcription.streaming (realtime
	// WebSocket).
	streaming transcriptionSupport
	// streamingDelay is capabilities.transcription.streamingDelay.
	streamingDelay transcriptionSupport
	// responseFormat is requestConfig.transcription.responseFormat, the
	// default response_format ("" = none).
	responseFormat string
	// chunkingStrategy is requestConfig.transcription.chunkingStrategy, the
	// default chunking_strategy ("" = none).
	chunkingStrategy string
	// languageField is requestConfig.transcription.languageField: how the
	// streaming session sends providerOptions.openai.language ("language" or
	// "languages", the latter as a one-element array).
	languageField string
}

var openaiTranscriptionModelEntries = map[string]transcriptionModelEntry{
	"whisper-1": {
		rest: transcriptionSupportYes, streaming: transcriptionSupportNo, streamingDelay: transcriptionSupportNo,
		responseFormat: "verbose_json", languageField: "language",
	},
	"gpt-4o-mini-transcribe": {
		rest: transcriptionSupportYes, streaming: transcriptionSupportNo, streamingDelay: transcriptionSupportNo,
		languageField: "language",
	},
	"gpt-4o-transcribe": {
		rest: transcriptionSupportYes, streaming: transcriptionSupportNo, streamingDelay: transcriptionSupportNo,
		languageField: "language",
	},
	"gpt-4o-transcribe-diarize": {
		rest: transcriptionSupportYes, streaming: transcriptionSupportNo, streamingDelay: transcriptionSupportNo,
		responseFormat: "diarized_json", chunkingStrategy: "auto", languageField: "language",
	},
	"gpt-transcribe": {
		rest: transcriptionSupportYes, streaming: transcriptionSupportYes, streamingDelay: transcriptionSupportNo,
		languageField: "languages",
	},
	"gpt-live-transcribe": {
		rest: transcriptionSupportNo, streaming: transcriptionSupportYes, streamingDelay: transcriptionSupportYes,
		languageField: "languages",
	},
	"gpt-realtime-whisper": {
		rest: transcriptionSupportNo, streaming: transcriptionSupportYes, streamingDelay: transcriptionSupportYes,
		languageField: "language",
	},
}

// transcriptionSnapshotModelIDs are the base IDs whose dated snapshots
// ("<base>-<suffix>") share the base entry (TS
// transcriptionSnapshotModelIds).
var transcriptionSnapshotModelIDs = []string{"gpt-transcribe", "gpt-live-transcribe", "gpt-realtime-whisper"}

// getOpenAITranscriptionModelEntry ports TS
// getOpenAITranscriptionModelEntry: an exact catalog entry, else the entry of
// a snapshot base ID, else an entry with every capability unknown and the
// default request config.
func getOpenAITranscriptionModelEntry(modelID string) transcriptionModelEntry {
	if entry, ok := openaiTranscriptionModelEntries[modelID]; ok {
		return entry
	}
	for _, base := range transcriptionSnapshotModelIDs {
		if strings.HasPrefix(modelID, base+"-") {
			return openaiTranscriptionModelEntries[base]
		}
	}
	return transcriptionModelEntry{languageField: "language"}
}

// SupportsStreamingTranscription reports whether modelID streams
// transcripts over the OpenAI realtime WebSocket (TS doStream's
// `capabilities.transcription.streaming === true`): gpt-realtime-whisper,
// gpt-live-transcribe, gpt-transcribe and their dated snapshots
// (TS 8375141ef4).
func SupportsStreamingTranscription(modelID string) bool {
	return getOpenAITranscriptionModelEntry(modelID).streaming == transcriptionSupportYes
}
