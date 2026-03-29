package mcp

import (
	"strings"
	"testing"
)

// TestMCPOAuthStateParamValidation verifies that a mismatched state param
// causes ValidateOAuthState to return an error (CSRF protection).
func TestMCPOAuthStateParamValidation(t *testing.T) {
	err := ValidateOAuthState("returned-state-xyz", "sent-state-abc")
	if err == nil {
		t.Fatal("expected error for mismatched state params, got nil")
	}
	if !strings.Contains(err.Error(), "CSRF") {
		t.Errorf("error message should mention CSRF, got: %q", err.Error())
	}
}

// TestMCPOAuthStateParamMatchSucceeds verifies that a matching state param
// causes ValidateOAuthState to return nil (authorization proceeds normally).
func TestMCPOAuthStateParamMatchSucceeds(t *testing.T) {
	state := "random-state-1234567890"
	if err := ValidateOAuthState(state, state); err != nil {
		t.Errorf("expected nil for matching state params, got: %v", err)
	}
}

// TestMCPOAuthStateEmptyVsNonEmpty verifies that an empty returned state does
// not match a non-empty sent state.
func TestMCPOAuthStateEmptyVsNonEmpty(t *testing.T) {
	err := ValidateOAuthState("", "some-state")
	if err == nil {
		t.Fatal("expected error when returned state is empty, got nil")
	}
}
