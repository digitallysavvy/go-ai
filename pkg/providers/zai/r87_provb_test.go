package zai

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS zaiErrorStructure: zaiErrorSchema is z.union([{code, message},
// {error: {code, message}}]) (the first member wins and strips `error`), and
// errorToMessage reads `error.message` only for the second; message is a
// required string, so "" is a message.
func TestR87provbZaiErrorMessage(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"message":"","code":1}}`:          "",
		`{"message":"","code":"x"}`:                  "",
		`{"message":"top","error":{"message":"in"}}`: "top",
		`{"error":{"message":"in"}}`:                 "in",
	} {
		err := parseZaiProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != want {
			t.Errorf("%s: err = %#v, want message %q", body, err, want)
		}
	}
	if err := parseZaiProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"code":1}`)}); err != nil {
		t.Errorf("no message: err = %#v, want nil", err)
	}
}
