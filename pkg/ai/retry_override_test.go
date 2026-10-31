package ai

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// ProviderError.Retryable (TS APICallError.isRetryable) decides retries when
// set, whatever the status code says.
func TestIsGatewayCallRetryable_HonorsRetryableOverride(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"424 marked retryable", &providererrors.ProviderError{StatusCode: 424, Retryable: &yes}, true},
		{"500 marked not retryable", &providererrors.ProviderError{StatusCode: 500, Retryable: &no}, false},
		{"429 default", &providererrors.ProviderError{StatusCode: 429}, true},
		{"400 default", &providererrors.ProviderError{StatusCode: 400}, false},
		{"no status default", &providererrors.ProviderError{}, false},
		{"plain error", errors.New("x"), false},
	}
	for _, tc := range cases {
		if got := isGatewayCallRetryable(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
