package elevenlabs

import "testing"

// elevenlabs-speech-model.test.ts (TS 3e5a12334b): outputFormat "wav" is
// sent as ElevenLabs' wav_44100 for speech, dialogue, music and sound
// effects.
func TestR85provaElevenLabsWavOutputFormat(t *testing.T) {
	if got := mapElevenLabsOutputFormat("wav"); got != "wav_44100" {
		t.Fatalf("wav -> %q", got)
	}
	if got := speechOutputFormat("wav"); got != "wav_44100" {
		t.Fatalf("speechOutputFormat(wav) = %q", got)
	}
	if got := mapElevenLabsOutputFormat("wav_22050"); got != "wav_22050" {
		t.Fatalf("native format changed: %q", got)
	}
}
