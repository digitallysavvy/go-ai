package revai

import (
	"errors"
	"net/http"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS lets a transport failure propagate unchanged and keeps the failed
// response body, data and lower-case headers on an HTTP error (APICallError).
func TestHandleErrorKeepsTransportFailuresAndResponseData(t *testing.T) {
	sentinel := errors.New("Cannot connect to API: boom")
	if got := handleError(sentinel); got != sentinel {
		t.Fatalf("transport failure was wrapped: %v", got)
	}

	body := `{"error":{"message":"m","code":400}}`
	got := handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Headers: http.Header{"X-Request-Id": {"r1"}}, Body: []byte(body)})
	var perr *providererrors.ProviderError
	if !errors.As(got, &perr) || perr.StatusCode != 400 || perr.ResponseBody != body || perr.Data == nil || perr.ResponseHeaders["x-request-id"] != "r1" {
		t.Fatalf("got %#v", perr)
	}
}

// A handler message is final: an empty one is kept, and a download failure
// with no HTTP status propagates unchanged.
func TestDownloadErrorsKeepEmptyMessageAndStatuslessFailures(t *testing.T) {
	var perr *providererrors.ProviderError
	dl := &providererrors.DownloadError{URL: "u", StatusCode: 400, Body: []byte(`{"error":{"message":"","code":400}}`)}
	if !errors.As(handleError(dl), &perr) || perr.Message != "" || perr.StatusCode != 400 {
		t.Fatalf("empty message replaced: %#v", perr)
	}
	statusless := providererrors.NewDownloadError("u", 0, "", "Invalid URL: u", nil)
	if got := handleError(statusless); got != error(statusless) {
		t.Fatalf("statusless download error was wrapped: %v", got)
	}
}

// JS trim keeps U+0085 and strips NBSP/BOM, unlike Go's TrimSpace.
func TestSegmentTextTrimFollowsJSWhitespace(t *testing.T) {
	val := func(s string) revaiNullableString { return revaiNullableString{Str: s, Set: true} }
	ts, end := 0.0, 1.0
	_, segments, _ := convertRevaiTranscript(revaiTranscriptResponse{Monologues: []revaiTranscriptMonologue{{
		Elements: []revaiTranscriptElement{{Type: "text", Value: val("\u0085hi\u00a0\ufeff"), TS: &ts, EndTS: &end}},
	}}})
	if len(segments) != 1 || segments[0].Text != "\u0085hi" {
		t.Fatalf("segments = %+v", segments)
	}
}
