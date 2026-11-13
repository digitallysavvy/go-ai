package elevenlabs

import "testing"

// TestR94integWebSocketURLsAreWHATWG: TS toWebSocketUrl(config.url({path}))
// is new URL (cr-r94-provb-to-prova). Expected values from Node 24.
func TestR94integWebSocketURLsAreWHATWG(t *testing.T) {
	const base = "https://API.Example.COM:443/a/./b"
	u, err := toWebSocketURL(base, "/v1/text-to-speech/voice/stream-input")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.String(), "wss://api.example.com/a/b/v1/text-to-speech/voice/stream-input"; got != want {
		t.Errorf("speech: %s, want %s", got, want)
	}
	u, err = buildElevenLabsRealtimeURL(base, "scribe_v2", "pcm_16000", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.String(), "wss://api.example.com/a/b/v1/speech-to-text/realtime?model_id=scribe_v2&audio_format=pcm_16000"; got != want {
		t.Errorf("transcription: %s, want %s", got, want)
	}
	if _, err := toWebSocketURL("not a url", "/x"); err == nil || err.Error() != "Invalid URL" {
		t.Errorf("speech invalid base: err = %v, want Invalid URL", err)
	}
	if _, err := buildElevenLabsRealtimeURL("not a url", "m", "f", nil, nil); err == nil || err.Error() != "Invalid URL" {
		t.Errorf("transcription invalid base: err = %v, want Invalid URL", err)
	}
}
