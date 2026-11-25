package prodia

import (
	"strings"
	"testing"
)

// TS's unnamed-part fallback (`else if partContentType.startsWith('video/')`)
// replaces an output captured earlier.
func TestParseMultipartResponseLaterUnnamedVideoPartReplacesOutput(t *testing.T) {
	b := "BOUND"
	body := strings.Join([]string{
		"--" + b,
		`Content-Disposition: form-data; name="job"`,
		"Content-Type: application/json",
		"",
		`{"id":"j1"}`,
		"--" + b,
		`Content-Disposition: form-data; name="output"; filename="o.mp4"`,
		"Content-Type: video/mp4",
		"",
		"FIRST",
		"--" + b,
		`Content-Disposition: form-data; name="extra"`,
		"Content-Type: video/webm",
		"",
		"SECOND",
		"--" + b + "--",
		"",
	}, "\r\n")
	_, data, mt, err := parseMultipartResponse("multipart/form-data; boundary="+b, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "SECOND" || mt != "video/webm" {
		t.Fatalf("got %q %q, want SECOND video/webm", data, mt)
	}
}
