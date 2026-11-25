package toolwebfetch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

// r86btoolsHeaderCase is one raw HTTP response and what TS saw for it.
type r86btoolsHeaderCase struct {
	Name string `json:"name"`
	Raw  string `json:"raw"`
	TS   struct {
		ContentType *string `json:"contentType"`
		MediaType   *string `json:"mediaType"`
		Kind        string  `json:"kind"`
		Text        *string `json:"text"`
		Location    string  `json:"location"`
		Challenged  *bool   `json:"challenged"`
	} `json:"ts"`
}

// r86btoolsReadResponse parses a raw response the way Go's HTTP client does.
func r86btoolsReadResponse(t *testing.T, raw string) func() *http.Response {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	return func() *http.Response {
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(b)), nil)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
}

// V061 item 25 (+ the fetch Headers.get class): TS reads Content-Type,
// Location and cf-mitigated through fetch's Headers.get, which joins repeated
// lines with ", " and reads each byte as one character, and its regexes use
// JS `\s` (a raw 0xA0 byte is a no-break space). Recorded with node fetch
// against the same raw bytes (testdata/r86b_tools_fetch_headers_ts.json).
func TestR86btoolsFetchHeadersMatchTS(t *testing.T) {
	data, err := os.ReadFile("testdata/r86b_tools_fetch_headers_ts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []r86btoolsHeaderCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("no cases")
	}
	kinds := map[contentKind]string{kindHTML: "html", kindText: "text", kindUnsupported: "unsupported"}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			first := r86btoolsReadResponse(t, c.Raw)
			var requested []string
			ft := serve(func(req *http.Request) *http.Response {
				requested = append(requested, req.URL.String())
				if len(requested) == 1 {
					return first()
				}
				return response(200, "ok", "Content-Type", "text/plain")
			})
			res, err := fetchWebResource(context.Background(), "https://example.com/dir/page", nil, 1<<20, ft, domainFilter{})
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case c.TS.Location != "":
				if len(requested) != 2 || requested[1] != c.TS.Location {
					t.Fatalf("requested %q, want the redirect to %q", requested, c.TS.Location)
				}
			case c.TS.Challenged != nil:
				challenged := res.message == "The request was blocked by bot protection (HTTP 403)."
				if res.errCode != errHTTP || challenged != *c.TS.Challenged {
					t.Fatalf("result %+v, want challenged = %v", res, *c.TS.Challenged)
				}
			default:
				if res.errCode != "" || c.TS.ContentType == nil || res.contentType != *c.TS.ContentType {
					t.Fatalf("contentType %q (%+v), want %q", res.contentType, res, *c.TS.ContentType)
				}
				mediaType := getMediaType(res.contentType)
				if mediaType != *c.TS.MediaType {
					t.Fatalf("mediaType %q, want %q", mediaType, *c.TS.MediaType)
				}
				kind := classifyContent(mediaType, res.body)
				if kinds[kind] != c.TS.Kind {
					t.Fatalf("kind %s, want %s", kinds[kind], c.TS.Kind)
				}
				if text := decodeText(res.body, res.contentType, kind == kindHTML); c.TS.Text == nil || text != *c.TS.Text {
					t.Fatalf("text %q, want %v", text, *c.TS.Text)
				}
			}
		})
	}
}

// TS /https?:\/\/[^\s<>"'`]+/gi has no u flag: the s of https never matches
// U+017F (Go (?i) folds it), and every JS \s rune ends a URL.
func TestR86btoolsURLPatternMatchesJS(t *testing.T) {
	cases := map[string][]string{
		"see http\u017f://example.com/x":   nil,
		"see HTTPS://example.com/a\u00a0b": {"HTTPS://example.com/a"},
		"see https://example.com/a\u2028b": {"https://example.com/a"},
		"see https://example.com/a\u0085b": {"https://example.com/a\u0085b"},
	}
	for in, want := range cases {
		got := urlPattern.FindAllString(in, -1)
		if len(got) != len(want) || (len(got) > 0 && got[0] != want[0]) {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
