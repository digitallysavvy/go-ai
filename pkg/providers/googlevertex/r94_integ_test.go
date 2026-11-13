package googlevertex

import "testing"

// TestR94integLiveWebSocketURLIsWHATWG: TS new URL(`wss://${vertexHost(
// location)}/ws/${path}`): the host is lower-cased and a location that makes
// no valid host is "Invalid URL" (cr-r94-provb-to-provg). Expected from
// Node 24.
func TestR94integLiveWebSocketURLIsWHATWG(t *testing.T) {
	p := &Provider{}
	got, err := geminiLiveWebSocketURL(p, "Europe-West4")
	if err != nil {
		t.Fatal(err)
	}
	if want := "wss://europe-west4-aiplatform.googleapis.com/ws/" + liveGeminiTranscriptionWebSocketPath; got != want {
		t.Errorf("url = %s, want %s", got, want)
	}
	if _, err := geminiLiveWebSocketURL(p, "bad host"); err == nil || err.Error() != "Invalid URL" {
		t.Errorf("invalid location: err = %v, want Invalid URL", err)
	}
}
