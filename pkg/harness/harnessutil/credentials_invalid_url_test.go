package harnessutil

import (
	"strings"
	"testing"
)

// A base URL can carry credentials, so the error must not echo it. TS throws
// Node's "Invalid URL" TypeError, whose message doesn't include the input.
func TestCreateCredentialRequestTransformation_InvalidURLNotEchoed(t *testing.T) {
	secret := "sk-ant-secret-token"
	_, err := CreateCredentialRequestTransformation(CreateCredentialRequestTransformationOptions{
		MatchURL:         "://user:" + secret + "@",
		MatchHeaders:     map[string]string{"x-api-key": "placeholder"},
		TransformHeaders: map[string]string{"x-api-key": secret},
	})
	if err == nil {
		t.Fatal("expected an error for an invalid URL")
	}
	if err.Error() != "Invalid URL" || strings.Contains(err.Error(), secret) {
		t.Fatalf("err = %q, want exactly %q", err.Error(), "Invalid URL")
	}
}
