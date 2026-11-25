package gladia

import (
	"errors"
	"reflect"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS gladiaFailedResponseHandler on the result_url poll: the APICallError keeps
// the response headers (retry-after) and data is the schema-parsed body
// ({error: {message, code}}, other keys stripped); a body off the schema has
// no data.
func TestR89provcPollErrorCarriesHeadersAndData(t *testing.T) {
	dl := &providererrors.DownloadError{URL: "u", StatusCode: 429,
		Headers: map[string][]string{"Retry-After": {"2"}},
		Body:    []byte(`{"error":{"message":"slow","code":429,"extra":1},"request_id":"r"}`)}
	var perr *providererrors.ProviderError
	if !errors.As(handleError(dl), &perr) {
		t.Fatal("no ProviderError")
	}
	want := map[string]interface{}{"error": map[string]interface{}{"message": "slow", "code": float64(429)}}
	if perr.Message != "slow" || perr.ResponseHeaders["Retry-After"] != "2" || !reflect.DeepEqual(perr.Data, want) {
		t.Fatalf("provider error = %+v", perr)
	}
	off := &providererrors.DownloadError{URL: "u", StatusCode: 502, StatusText: "Bad Gateway", Body: []byte(`<html>`)}
	if !errors.As(handleError(off), &perr) || perr.Data != nil || perr.Message != "Bad Gateway" {
		t.Fatalf("off-schema provider error = %+v", perr)
	}
}
