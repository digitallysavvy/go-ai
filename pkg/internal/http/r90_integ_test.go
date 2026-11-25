package http

import (
	"io"
	"strings"
	"testing"
)

// cr-r90-prova-to-integrator: an inline file TS sends with postFormDataToApi
// (FormData) gets the FormData part header (Blob type lowercased, `"` as
// %22), while a streamed part keeps the postMultipartStreamToApi header.
func TestR90integMultipartFormDataPart(t *testing.T) {
	body, _, err := NewMultipartStreamBody([]MultipartStreamPart{
		{IsFile: true, Name: "file", Filename: `a"b.pdf`, MediaType: "Application/PDF", Content: strings.NewReader("x"), FormData: true},
		{IsFile: true, Name: "file", Filename: `a"b.pdf`, MediaType: "Application/PDF", Content: strings.NewReader("y")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close() //nolint:errcheck
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		"Content-Disposition: form-data; name=\"file\"; filename=\"a%22b.pdf\"\r\nContent-Type: application/pdf\r\n\r\nx\r\n",
		"Content-Disposition: form-data; name=\"file\"; filename=\"a\\\"b.pdf\"\r\nContent-Type: Application/PDF\r\n\r\ny\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("body = %q, want it to contain %q", got, want)
		}
	}
}
