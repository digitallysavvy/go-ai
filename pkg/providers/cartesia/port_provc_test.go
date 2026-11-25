package cartesia

import "testing"

// TS b7f313223f / 373b075cdf catalog refactor (ruling 13): the streaming
// predicate moved into getCartesiaTranscriptionModelEntry and must not have
// moved for unknown IDs (TS: modelId !== 'ink-whisper' &&
// !modelId.startsWith('ink-whisper-')).
func TestPortProvcStreamingPredicateOnUnknownIDs(t *testing.T) {
	for id, want := range map[string]bool{
		"ink-whisper":            false,
		"ink-whisper-2025-06-04": false,
		"ink-whispering":         true, // not the family: no trailing dash
		"future-model":           true,
		"":                       true,
		"sonic-3":                true,
	} {
		if got := isStreamingTranscriptionModelID(id); got != want {
			t.Errorf("isStreamingTranscriptionModelID(%q) = %v, want %v", id, got, want)
		}
	}
}
