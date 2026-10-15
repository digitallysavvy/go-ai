package streaming

import (
	"io"
	"reflect"
	"strings"
	"testing"
)

// Mirrors eventsource-parser (used by TS parseJsonEventStream): an event is
// dispatched when it has a data field, even an empty one; a field line with no
// colon is a field with an empty value; CR, LF and CRLF all end a line; a
// leading BOM is dropped.
func TestSSEParserMatchesEventSourceParser(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string // event|data
	}{
		{"empty data line", "data:\n\n", []string{"|"}},
		{"empty data line with space", "data: \n\ndata: x\n\n", []string{"|", "|x"}},
		{"data without colon", "data\n\ndata: x\n\n", []string{"|", "|x"}},
		{"event without data is not dispatched", "event: ping\n\ndata: x\n\n", []string{"|x"}},
		{"bare CR", "data: a\r\rdata: b\r\r", []string{"|a", "|b"}},
		{"CRLF", "data: a\r\n\r\ndata: b\r\n\r\n", []string{"|a", "|b"}},
		{"BOM", "\xef\xbb\xbfdata: a\n\n", []string{"|a"}},
		{"multi data lines", "data: a\ndata: b\n\n", []string{"|a\nb"}},
		{"comment ignored", ": hi\ndata: a\n\n", []string{"|a"}},
		{"EOF without blank line", "event: e\ndata: a", []string{"e|a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewSSEParser(strings.NewReader(tc.in))
			var got []string
			for {
				e, err := p.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, e.Event+"|"+e.Data)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
