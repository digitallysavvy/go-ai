package zai

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provbZaiErrorUnionBranches: TS zaiErrorSchema tries {code, message}
// then {error: {code, message}}; a member whose code is not a number or
// string, or whose message is not a string, does not match (zai-error.ts).
func TestR90provbZaiErrorUnionBranches(t *testing.T) {
	for body, want := range map[string]string{
		`{"message":"top","code":true,"error":{"message":"in"}}`: "in",
		`{"message":5,"error":{"message":"in","code":2}}`:        "in",
		`{"code":null,"message":"m"}`:                            "m",
	} {
		err := parseZaiProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != want {
			t.Errorf("%s: err = %#v, want message %q", body, err, want)
		}
	}
	for _, body := range []string{`{"message":"m","code":true}`, `{"error":{"message":"in","code":{}}}`} {
		if err := parseZaiProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}); err != nil {
			t.Errorf("%s: err = %#v, want nil", body, err)
		}
	}
}
