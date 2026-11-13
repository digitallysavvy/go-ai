package cartesia

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestR96provcRealtimeCombinedDurationOverflow: transcript durations whose
// sum overflows the double range give duration null in the combined raw, as
// JSON.stringify writes Infinity; the raw stays valid JSON.
func TestR96provcRealtimeCombinedDurationOverflow(t *testing.T) {
	m := newTestRealtimeModel("https://api.cartesia.ai")
	parse(t, m, `{"type":"transcript","request_id":"r","text":"a","is_final":true,"duration":1.7e308}`)
	parse(t, m, `{"type":"transcript","request_id":"r","text":"b","is_final":true,"duration":1.7e308}`)
	e := parse(t, m, `{"type":"flush_done","request_id":"r"}`)
	if len(e) != 2 {
		t.Fatalf("events = %+v", e)
	}
	if !json.Valid(e[1].Raw) || !strings.HasSuffix(string(e[1].Raw), `,"duration":null}`) {
		t.Errorf("raw = %s", e[1].Raw)
	}
}
