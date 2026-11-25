package xai

import (
	"encoding/json"
	"testing"
)

// TS mapWebSearchAction: the action must parse against the discriminated
// union (an unknown type yields {}), a source is any {type:'url', url:string}
// (an empty url included), and a null url/pattern stays null.
func TestR59MapWebSearchActionMatchesTSSchema(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"unknown action type drops the sources": {
			in:   `{"type":"other","sources":[{"type":"url","url":"https://a.test"}]}`,
			want: `{}`,
		},
		"empty source url is kept": {
			in:   `{"type":"search","query":"q","sources":[{"type":"url","url":""},{"type":"api","url":"x"}]}`,
			want: `{"action":{"query":"q","type":"search"},"sources":[{"type":"url","url":""}]}`,
		},
		"null url and pattern stay null": {
			in:   `{"type":"find_in_page","url":null,"pattern":null}`,
			want: `{"action":{"pattern":null,"type":"findInPage","url":null}}`,
		},
		"null open_page url stays null": {
			in:   `{"type":"open_page","url":null}`,
			want: `{"action":{"type":"openPage","url":null}}`,
		},
		"absent url stays absent": {
			in:   `{"type":"open_page"}`,
			want: `{"action":{"type":"openPage"}}`,
		},
		"null sources": {
			in:   `{"type":"search","sources":null}`,
			want: `{"action":{"type":"search"}}`,
		},
		"another branch's field of any type is ignored": {
			in:   `{"type":"search","query":"q","pattern":5,"url":{}}`,
			want: `{"action":{"query":"q","type":"search"}}`,
		},
		"open_page ignores query": {
			in:   `{"type":"open_page","url":"https://a.test","query":1}`,
			want: `{"action":{"type":"openPage","url":"https://a.test"}}`,
		},
		"own branch field of the wrong type fails the parse": {
			in:   `{"type":"find_in_page","url":"https://a.test","pattern":5}`,
			want: `{}`,
		},
		"wrong source url type is dropped": {
			in:   `{"type":"search","sources":[{"type":"url","url":3}]}`,
			want: `{"action":{"type":"search"}}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := json.Marshal(mapWebSearchAction(json.RawMessage(tc.in)))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("mapWebSearchAction(%s) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}
