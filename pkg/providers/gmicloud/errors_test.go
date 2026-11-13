package gmicloud

import (
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ported from gmicloud-error.test.ts "falls back to the outer message when
// details is absent".
func TestUnwrapGmicloudDetailsMessageFallsBackWhenDetailsAbsent(t *testing.T) {
	if got := unwrapGmicloudDetailsMessage(""); got != "" {
		t.Fatalf("unwrapGmicloudDetailsMessage(\"\") = %q, want \"\"", got)
	}
}

// Ported from gmicloud-error.test.ts "falls back to the outer message when
// details is not JSON".
func TestUnwrapGmicloudDetailsMessageFallsBackWhenDetailsNotJSON(t *testing.T) {
	if got := unwrapGmicloudDetailsMessage("<html>nginx</html>"); got != "" {
		t.Fatalf("unwrapGmicloudDetailsMessage(non-JSON) = %q, want \"\"", got)
	}
}

// Ported from gmicloud-error.test.ts "falls back to the outer message when
// details has no inner message".
func TestUnwrapGmicloudDetailsMessageFallsBackWhenNoInnerMessage(t *testing.T) {
	if got := unwrapGmicloudDetailsMessage(`{"error":{"code":"500"}}`); got != "" {
		t.Fatalf("unwrapGmicloudDetailsMessage(no inner message) = %q, want \"\"", got)
	}
}

// Verifies the full envelope's outer message is used when details doesn't
// unwrap to anything, mirroring the "banner" assertions in the TS "falls
// back" tests.
func TestParseGmicloudProviderErrorFallsBackToOuterMessage(t *testing.T) {
	err := &internalhttp.HTTPStatusError{
		StatusCode: 400,
		Body:       []byte(`{"error":{"message":"banner","details":"<html>nginx</html>"}}`),
	}
	parsed := parseGmicloudProviderError(err)
	if parsed == nil {
		t.Fatal("parseGmicloudProviderError() = nil, want a parsed error")
	}
	provErr, ok := parsed.(*providererrors.ProviderError)
	if !ok {
		t.Fatalf("parseGmicloudProviderError() type = %T, want *providererrors.ProviderError", parsed)
	}
	if provErr.Message != "banner" {
		t.Fatalf("Message = %q, want %q", provErr.Message, "banner")
	}
}

// Ported from gmicloud-error.test.ts "rejects GMI's plain-text 404 body,
// deferring to status text": a non-JSON top-level body must not be parsed as
// the gmicloud envelope, so the message is the HTTP status text.
func TestParseGmicloudProviderErrorRejectsPlainTextBody(t *testing.T) {
	err := &internalhttp.HTTPStatusError{
		StatusCode: 404,
		Body:       []byte("No matching target server found for model foo"),
	}
	parsed := parseGmicloudProviderError(err)
	provErr, ok := parsed.(*providererrors.ProviderError)
	if !ok || provErr.Message != "Not Found" || provErr.StatusCode != 404 {
		t.Fatalf("parseGmicloudProviderError(plain text body) = %v, want a 404 ProviderError with the status text", parsed)
	}
}

func TestParseGmicloudProviderErrorSchemaEdges(t *testing.T) {
	msg := func(body string) string {
		parsed := parseGmicloudProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)})
		return parsed.(*providererrors.ProviderError).Message
	}
	if got := msg(`{"error":{"message":""}}`); got != "" {
		t.Errorf("empty message = %q", got)
	}
	if got := msg(`{"error":{"message":"outer","details":"{\"error\":{\"message\":\" inner \"}}"}}`); got != " inner " {
		t.Errorf("inner message must be returned untrimmed, got %q", got)
	}
	if got := msg(`{"error":{"message":"outer","code":{"x":1}}}`); got != "Bad Request" {
		t.Errorf("object code is off schema, got %q", got)
	}
	if got := msg(`{"error":{"message":"outer","code":42}}`); got != "outer" {
		t.Errorf("numeric code is valid, got %q", got)
	}
}
