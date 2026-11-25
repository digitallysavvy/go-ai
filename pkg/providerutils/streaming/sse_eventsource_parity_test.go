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
// leading BOM is dropped. EventSourceParserStream (eventsource-parser 3.0.8,
// what TS provider-utils resolves) has no flush, so an event with no closing
// blank line at EOF, and a final line with no terminator (a lone trailing CR
// included), are never dispatched.
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
		{"bare CR", "data: a\r\rdata: b\r\r\n", []string{"|a", "|b"}},
		{"trailing bare CR is held back", "data: a\r\rdata: b\r\r", []string{"|a"}},
		{"CRLF", "data: a\r\n\r\ndata: b\r\n\r\n", []string{"|a", "|b"}},
		{"BOM", "\xef\xbb\xbfdata: a\n\n", []string{"|a"}},
		{"multi data lines", "data: a\ndata: b\n\n", []string{"|a\nb"}},
		{"comment ignored", ": hi\ndata: a\n\n", []string{"|a"}},
		{"EOF without blank line", "data: x\n\nevent: e\ndata: a\n", []string{"|x"}},
		{"EOF without line terminator", "data: x\n\nevent: e\ndata: a", []string{"|x"}},
		{"EOF after blank line", "event: e\ndata: a\n\n", []string{"e|a"}},
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
