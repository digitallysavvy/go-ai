package streaming

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"
)

// r85streamSSEEvent is the comparable projection of an SSEEvent.
type r85streamSSEEvent struct{ ID, Event, Data string }

// Expected events recorded from TS: each input fed as one chunk through
// TextDecoderStream and eventsource-parser 3.0.8 EventSourceParserStream (the
// pipeline of provider-utils parseJsonEventStream), Node v24.
func TestR85streamSSEFieldsMatchEventSourceParser(t *testing.T) {
	cases := []struct {
		name, hex string
		want      []r85streamSSEEvent
		retries   []int
	}{
		// An id containing NUL is ignored.
		{"nul id", "69643a206100620a646174613a20780a0a", []r85streamSSEEvent{{Data: "x"}}, []int{0}},
		// Invalid UTF-8 in id and event decodes to U+FFFD like data does.
		{"invalid utf-8 id and event", "69643a2031e0a00a6576656e743a2065ff0a646174613a20790a0a", []r85streamSSEEvent{{ID: "1�", Event: "e�", Data: "y"}}, []int{0}},
		// A leading mis-decoded BOM (the text "ï»¿") is dropped.
		{"mis-decoded bom", "c3afc2bbc2bf646174613a207a0a0a", []r85streamSSEEvent{{Data: "z"}}, []int{0}},
		// ... also after a real BOM, which the decoder drops first.
		{"bom then mis-decoded bom", "efbbbfc3afc2bbc2bf646174613a20770a0a", []r85streamSSEEvent{{Data: "w"}}, []int{0}},
		// retry accepts only ASCII digits (TS onRetry is called with 15 only).
		{"retry digits only", "72657472793a203132780a646174613a20720a0a72657472793a2031350a646174613a20730a0a", []r85streamSSEEvent{{Data: "r"}, {Data: "s"}}, []int{0, 15}},
	}
	for _, c := range cases {
		in, err := hex.DecodeString(c.hex)
		if err != nil {
			t.Fatal(err)
		}
		evs, err := ParseSSEStream(bytes.NewReader(in))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		var got []r85streamSSEEvent
		var retries []int
		for _, e := range evs {
			got = append(got, r85streamSSEEvent{e.ID, e.Event, e.Data})
			retries = append(retries, e.Retry)
		}
		if !reflect.DeepEqual(got, c.want) || !reflect.DeepEqual(retries, c.retries) {
			t.Errorf("%s: events %q retries %v, want %q %v", c.name, got, retries, c.want, c.retries)
		}
	}
}

func TestR85streamSSERetryOverflow(t *testing.T) {
	if _, ok := parseSSERetry("99999999999999999999999"); ok {
		t.Error("overflowing retry accepted")
	}
	if n, ok := parseSSERetry("0"); !ok || n != 0 {
		t.Errorf("retry 0 = %d, %v", n, ok)
	}
}
