package hume

import "testing"

// TS 4ec95dd39a catalog refactor (ruling 13): the output-format predicate
// moved into getHumeSpeechModelEntry. Hume has no selectable model, so the
// predicate cannot depend on the (empty or unknown) model ID.
func TestPortProvcOutputFormatPredicate(t *testing.T) {
	for format, want := range map[string]bool{"mp3": true, "pcm": true, "wav": true, "opus": false, "MP3": false, "": false} {
		if got := humeOutputFormats[format]; got != want {
			t.Errorf("humeOutputFormats[%q] = %v, want %v", format, got, want)
		}
	}
}
