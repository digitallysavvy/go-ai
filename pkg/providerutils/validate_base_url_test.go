package providerutils

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS validateBaseURL throws InvalidArgumentError('baseURL must be a
// non-empty string.').
func TestValidateBaseURLIsInvalidArgumentError(t *testing.T) {
	err := ValidateBaseURL("  ")
	if err != ErrEmptyBaseURL || err.Error() != "baseURL must be a non-empty string." {
		t.Fatalf("err = %v", err)
	}
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) || invalid.Field != "baseURL" {
		t.Fatalf("errors.As(*InvalidArgumentError) failed: %v", err)
	}
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatal("IsInvalidArgumentError must hold")
	}
	if ValidateBaseURL("") != nil || ValidateBaseURL("https://x") != nil {
		t.Fatal("unset and real URLs are valid")
	}
}
