package providerutils

import (
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestLoadAPIKey(t *testing.T) {
	t.Setenv("GOAI_TEST_API_KEY", "")
	if _, err := LoadAPIKey("", "GOAI_TEST_API_KEY", "Example"); !providererrors.IsLoadAPIKeyError(err) ||
		err.Error() != "Example API key is missing. Pass it using the 'apiKey' parameter or the GOAI_TEST_API_KEY environment variable." {
		t.Fatalf("missing key err = %v", err)
	}
	t.Setenv("GOAI_TEST_API_KEY", "env")
	if k, err := LoadAPIKey("", "GOAI_TEST_API_KEY", "Example"); err != nil || k != "env" {
		t.Fatalf("env key = %q, %v", k, err)
	}
	if k, err := LoadAPIKey("explicit", "GOAI_TEST_API_KEY", "Example"); err != nil || k != "explicit" {
		t.Fatalf("explicit key = %q, %v", k, err)
	}
	if k := APIKeyOrEnv("", "GOAI_TEST_API_KEY"); k != "env" {
		t.Fatalf("APIKeyOrEnv = %q", k)
	}
}
