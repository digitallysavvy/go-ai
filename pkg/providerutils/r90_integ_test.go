package providerutils

import (
	"bytes"
	"errors"
	"mime/multipart"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// cr-r90-provb-to-integrator 1: TS createStatusCodeErrorResponseHandler
// builds APICallError({message: statusText, statusCode, responseHeaders,
// responseBody}) with no data, even for a JSON body.
func TestR90integStatusCodeDownloadErrorHasNoData(t *testing.T) {
	err := StatusCodeDownloadError("p", &providererrors.DownloadError{StatusCode: 404, Body: []byte(`{"error":"x"}`)})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %T %v, want *ProviderError", err, err)
	}
	if pe.Message != "Not Found" || pe.Data != nil || pe.StatusCode != 404 || string(pe.ResponseBody) != `{"error":"x"}` {
		t.Fatalf("ProviderError = message %q data %#v status %d body %q", pe.Message, pe.Data, pe.StatusCode, pe.ResponseBody)
	}
}

// cr-r90-prova-to-integrator: part headers as TS FormData serializes them.
// Expected values recorded with Node 24 (undici): new Response(formData).text()
// for fd.append('fi"e\nld', new Blob([..], {type: 'Application/PDF'}),
// 'a"b\\c\nd\re.pdf'), fd.append('f2', new Blob([..], {type: 'tëxt/plain'}),
// ''), fd.append('f3', new Blob([..], {type: ''}), 'blob') and
// fd.append('s"x', 'v\nw').
func TestR90integFormDataPartHeaders(t *testing.T) {
	for _, tc := range []struct {
		field, filename, mediaType string
		disposition, contentType   string
	}{
		{"fi\"e\nld", "a\"b\\c\nd\re.pdf", "Application/PDF", `form-data; name="fi%22e%0D%0Ald"; filename="a%22b\c%0Ad%0De.pdf"`, "application/pdf"},
		{"f2", "", "tëxt/plain", `form-data; name="f2"`, "application/octet-stream"},
		{"f3", "blob", "", `form-data; name="f3"; filename="blob"`, "application/octet-stream"},
		{"f4", "ü.txt", "text/plain; charset=UTF-8", `form-data; name="f4"; filename="ü.txt"`, "text/plain; charset=utf-8"},
	} {
		h := FormDataFilePartHeader(tc.field, tc.filename, tc.mediaType)
		if got := h.Get("Content-Disposition"); got != tc.disposition {
			t.Errorf("%q: Content-Disposition = %q, want %q", tc.field, got, tc.disposition)
		}
		if got := h.Get("Content-Type"); got != tc.contentType {
			t.Errorf("%q: Content-Type = %q, want %q", tc.field, got, tc.contentType)
		}
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := WriteFormDataField(w, `s"x`, "v\nw"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if want := "Content-Disposition: form-data; name=\"s%22x\"\r\n\r\nv\r\nw\r\n"; !strings.Contains(buf.String(), want) {
		t.Fatalf("field part = %q, want it to contain %q", buf.String(), want)
	}
}
