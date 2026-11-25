package deepgram

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR94integStreamingURLIsWHATWG: TS toWebSocketUrl(config.url({path:
// '/v1/listen'})) is new URL (cr-r94-provb-to-provc). Expected from Node 24.
func TestR94integStreamingURLIsWHATWG(t *testing.T) {
	var warnings []types.Warning
	got, err := deepgramStreamingURL("https://API.Example.COM:443/a/./b", "nova-3", "linear16", 16000, nil, &warnings)
	if err != nil {
		t.Fatal(err)
	}
	if want := "wss://api.example.com/a/b/v1/listen?model=nova-3&encoding=linear16&sample_rate=16000"; got != want {
		t.Errorf("url = %s, want %s", got, want)
	}
	if _, err := deepgramStreamingURL("not a url", "m", "e", 1, nil, &warnings); err == nil || err.Error() != "Invalid URL" {
		t.Errorf("invalid base: err = %v, want Invalid URL", err)
	}
}
