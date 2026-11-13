package mistral

import "testing"

// TestR94integRealtimeURLIsWHATWG: TS toWebSocketUrl(`${baseURL}/audio/
// transcriptions/realtime`) is new URL (cr-r94-provb-to-provg). Expected
// from Node 24 (the Go base URL carries no /v1).
func TestR94integRealtimeURLIsWHATWG(t *testing.T) {
	got, err := mistralRealtimeTranscriptionURL("https://API.Example.COM:443/a/./b", "voxtral")
	if err != nil {
		t.Fatal(err)
	}
	if want := "wss://api.example.com/a/b/v1/audio/transcriptions/realtime?model=voxtral"; got != want {
		t.Errorf("url = %s, want %s", got, want)
	}
	if _, err := mistralRealtimeTranscriptionURL("not a url", "m"); err == nil || err.Error() != "Invalid URL" {
		t.Errorf("invalid base: err = %v, want Invalid URL", err)
	}
}
