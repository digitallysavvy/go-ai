package mcp

import (
	"crypto/subtle"
	"fmt"
)

// ValidateOAuthState compares the state parameter returned in an OAuth callback
// against the state value that was originally sent in the authorization request.
//
// The comparison is performed with crypto/subtle.ConstantTimeCompare to prevent
// timing-based side-channel attacks. A mismatch indicates a possible CSRF attack
// and an error is returned.
//
// Usage in an authorization-code callback handler:
//
//	if err := ValidateOAuthState(returnedState, sentState); err != nil {
//	    http.Error(w, err.Error(), http.StatusBadRequest)
//	    return
//	}
func ValidateOAuthState(returnedState, sentState string) error {
	if subtle.ConstantTimeCompare([]byte(returnedState), []byte(sentState)) != 1 {
		return fmt.Errorf("mcp oauth: state parameter mismatch — possible CSRF attack")
	}
	return nil
}
