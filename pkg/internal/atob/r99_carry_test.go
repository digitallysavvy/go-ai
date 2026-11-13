package atob_test

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/atob"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/speechstream"
)

// Node 24 atob after the TS convertBase64ToUint8Array "-"/"_" mapping
// (node -e atob(...)): name InvalidCharacterError and these messages.
func TestR99carryAtobErrorMessage(t *testing.T) {
	for s, want := range map[string]string{
		"*":      "Invalid character",
		"AAAAA":  "The string to be decoded is not correctly encoded.",
		"AA=A":   "Invalid character",
		"AAA===": "Invalid character",
		"A":      "The string to be decoded is not correctly encoded.",
		"AB C=D": "Invalid character",
	} {
		err := atob.Error(s)
		if err.Error() != want || err.ErrorName() != "InvalidCharacterError" {
			t.Errorf("Error(%q) = %q (%s), want %q", s, err.Error(), err.ErrorName(), want)
		}
	}
	// speechstream's exported type is the shared one.
	var target *speechstream.InvalidCharacterError
	if !errors.As(error(atob.Error("*")), &target) {
		t.Fatal("speechstream.InvalidCharacterError does not match atob's error")
	}
}
