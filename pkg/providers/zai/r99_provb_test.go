package zai

import (
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// TestR99provbErrorInfiniteCode: TS zaiErrorDetailsSchema's code is
// z.union([z.number(), z.string()]) and z.number() rejects the Infinity
// JSON.parse reads for 1e400, so neither union member matches and the
// message is the status text. Go took the body's message.
func TestR99provbErrorInfiniteCode(t *testing.T) {
	for _, body := range []string{`{"code":1e400,"message":"m"}`, `{"error":{"code":-1e400,"message":"m"}}`} {
		if err := parseZaiProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}); err != nil {
			t.Errorf("%s: err = %v, want no schema match", body, err)
		}
	}
	if err := parseZaiProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"code":1200,"message":"m"}`)}); err == nil {
		t.Error("a finite numeric code must match")
	}
}
