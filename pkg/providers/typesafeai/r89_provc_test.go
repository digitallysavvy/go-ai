package typesafeai

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS typesafeFailedResponseHandler (createJsonErrorResponseHandler): data is
// the zod-parsed body (declared keys only, an object `error` stripped to its
// message, null kept); a body off the schema has no data. The HTTP cause stays
// in the chain.
func TestR89provcErrorDataAndCause(t *testing.T) {
	m := &DecisionModel{}
	cause := &internalhttp.HTTPStatusError{StatusCode: 429, Headers: http.Header{"Retry-After": {"1"}},
		Body: []byte(`{"error":{"message":"slow","code":7},"detail":null,"extra":true}`)}
	var pe *providererrors.ProviderError
	err := m.handleError(cause)
	if !errors.As(err, &pe) {
		t.Fatalf("no ProviderError: %v", err)
	}
	want := map[string]interface{}{"error": map[string]interface{}{"message": "slow"}, "detail": nil}
	if pe.Message != "slow" || !reflect.DeepEqual(pe.Data, want) || pe.ResponseHeaders["Retry-After"] != "1" {
		t.Fatalf("provider error = %+v", pe)
	}
	var httpErr *internalhttp.HTTPStatusError
	if !errors.As(err, &httpErr) {
		t.Fatal("HTTP cause dropped")
	}
	off := &internalhttp.HTTPStatusError{StatusCode: 502, Body: []byte(`{"message":5}`)}
	if !errors.As(m.handleError(off), &pe) || pe.Data != nil || pe.Message != "Bad Gateway" {
		t.Fatalf("off-schema provider error = %+v", pe)
	}
}
