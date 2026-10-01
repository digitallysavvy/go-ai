package ai

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ports data-url.test.ts's getTextFromDataUrl cases (audit row c415657 / WG14).

func TestGetTextFromDataURL_MalformedDataURL(t *testing.T) {
	_, err := GetTextFromDataURL("not-a-data-url")
	var invalidArg *providererrors.InvalidArgumentError
	if err == nil {
		t.Fatal("expected an error")
	}
	if ok := errors.As(err, &invalidArg); !ok {
		t.Fatalf("error = %T, want *InvalidArgumentError", err)
	}
	if invalidArg.Message != "Invalid data URL format" {
		t.Fatalf("message = %q, want %q", invalidArg.Message, "Invalid data URL format")
	}
}

func TestGetTextFromDataURL_CannotBeDecoded(t *testing.T) {
	// Not valid base64 (illegal characters/padding), mirroring the TS test's
	// mocked atob() throwing on decode.
	_, err := GetTextFromDataURL("data:text/plain;base64,!!!not-base64!!!")
	var invalidArg *providererrors.InvalidArgumentError
	if err == nil {
		t.Fatal("expected an error")
	}
	if ok := errors.As(err, &invalidArg); !ok {
		t.Fatalf("error = %T, want *InvalidArgumentError", err)
	}
	if invalidArg.Message != "Error decoding data URL" {
		t.Fatalf("message = %q, want %q", invalidArg.Message, "Error decoding data URL")
	}
}

func TestGetTextFromDataURL_DecodesBase64Text(t *testing.T) {
	got, err := GetTextFromDataURL("data:text/plain;base64,aGk=")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hi" {
		t.Fatalf("got %q, want %q", got, "hi")
	}
}

func TestGetTextFromDataURL_DecodesUTF8(t *testing.T) {
	cases := []struct {
		name   string
		base64 string
		want   string
	}{
		{"two-byte characters", "Y2Fmw6k=", "café"},
		{"three-byte characters", "5pel5pys6Kqe", "日本語"},
		{"four-byte characters", "ZW1vamkg8J+Zgg==", "emoji 🙂"},
		{"combining marks", "Q2FmZcyB", "Café"},
		{"multiline text", "Zmlyc3QgbGluZQpjYWbDqQrml6XmnKzoqp4g8J+Zgg==", "first line\ncafé\n日本語 🙂"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GetTextFromDataURL("data:text/plain;charset=utf-8;base64," + tc.base64)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetTextFromDataURL_DecodesNonUTF8Charset(t *testing.T) {
	got, err := GetTextFromDataURL("data:text/plain;charset=iso-8859-1;base64,Y2Fm6Q==")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "café" {
		t.Fatalf("got %q, want %q", got, "café")
	}
}

func TestGetTextFromDataURL_QuotedCharset(t *testing.T) {
	got, err := GetTextFromDataURL(`data:text/plain;charset="utf-8";base64,aGk=`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hi" {
		t.Fatalf("got %q, want %q", got, "hi")
	}
}
