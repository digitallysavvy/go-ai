package cartesia

import "testing"

// TestR94integStreamingURLIsWHATWG: TS toWebSocketUrl(new URL(path,
// baseURL)) keeps the base URL's origin and credentials, drops its path and
// query, and is the WHATWG serialization (cr-r94-provb-to-provc). Expected
// from Node 24.
func TestR94integStreamingURLIsWHATWG(t *testing.T) {
	u, err := buildCartesiaStreamingTranscriptionURL("https://User:Pw@API.Cartesia.AI:443/v1?x=1", "2025-04-16", "ink", "pcm_s16le", 16000, nil, "tok", false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := u.String(), "wss://User:Pw@api.cartesia.ai/stt/websocket?model=ink&encoding=pcm_s16le&sample_rate=16000&cartesia_version=2025-04-16&access_token=tok"; got != want {
		t.Errorf("url = %s, want %s", got, want)
	}
	if _, err := buildCartesiaStreamingTranscriptionURL("not a url", "v", "m", "e", 1, nil, "", false, false, nil); err == nil || err.Error() != "Invalid URL" {
		t.Errorf("invalid base: err = %v, want Invalid URL", err)
	}
}
