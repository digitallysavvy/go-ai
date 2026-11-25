package elevenlabs

import "testing"

// TestPortprovaCatalogRefactorUnknownIDs is the ruling-13 check for TS
// 04b91cce5d (elevenlabs catalog centralization): model-ID predicates keep
// the same results for known and unknown IDs.
func TestPortprovaCatalogRefactorUnknownIDs(t *testing.T) {
	cases := []struct {
		id                             string
		music, sfx, dialogue, realtime bool
	}{
		{"eleven_v4", false, false, true, false},
		{"eleven_v4_turbo", false, false, true, false},
		{"eleven_v3_conversational", false, false, true, false},
		{"eleven_v5_future", false, false, false, false},
		{"eleven_v3x_unknown", false, false, true, false},
		{"music_v9_unknown", true, false, false, false},
		{"eleven_text_to_sound_v9", false, true, false, false},
		{"scribe_v2_realtime", false, false, false, true},
		{"scribe_v9_realtime", false, false, false, false},
		{"unknown-model", false, false, false, false},
		{"", false, false, false, false},
	}
	for _, c := range cases {
		if got := isMusicModelID(c.id); got != c.music {
			t.Errorf("isMusicModelID(%q) = %v", c.id, got)
		}
		if got := isSoundEffectsModelID(c.id); got != c.sfx {
			t.Errorf("isSoundEffectsModelID(%q) = %v", c.id, got)
		}
		if got := isDialogueModelID(c.id); got != c.dialogue {
			t.Errorf("isDialogueModelID(%q) = %v", c.id, got)
		}
		if got := isRealtimeTranscriptionModelID(c.id); got != c.realtime {
			t.Errorf("isRealtimeTranscriptionModelID(%q) = %v", c.id, got)
		}
	}
}
