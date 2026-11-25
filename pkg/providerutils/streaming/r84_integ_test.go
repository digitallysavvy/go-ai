package streaming

import (
	"encoding/json"
	"strings"
	"testing"
)

// stream CR: TS TextDecoderStream turns each maximal invalid UTF-8 subpart
// into one U+FFFD (Node 24: x\xF0\x9F\x98y\xE0\x80z is x\uFFFDy\uFFFD\uFFFDz);
// encoding/json alone writes one per byte. One leading BOM is dropped.
func TestR84integSSEDataDecodedLikeTextDecoder(t *testing.T) {
	p := NewSSEParser(strings.NewReader("\xEF\xBB\xBFdata: {\"v\":\"x\xF0\x9F\x98y\xE0\x80z\"}\n\n"))
	ev, err := p.Next()
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ V string }
	if err := json.Unmarshal([]byte(ev.Data), &v); err != nil || v.V != "x\uFFFDy\uFFFD\uFFFDz" {
		t.Fatalf("data %q -> %q (%v)", ev.Data, v.V, err)
	}
}
