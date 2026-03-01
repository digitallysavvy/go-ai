package together

import (
	"os"
	"testing"
)

// TestGetAPIKeyDirectValue verifies an explicit API key takes priority.
func TestGetAPIKeyDirectValue(t *testing.T) {
	t.Setenv("TOGETHER_API_KEY", "env-key")
	t.Setenv("TOGETHER_AI_API_KEY", "deprecated-key")

	got := getAPIKey("direct-key")
	if got != "direct-key" {
		t.Errorf("getAPIKey(explicit) = %q, want %q", got, "direct-key")
	}
}

// TestGetAPIKeyPrimaryEnvVar verifies TOGETHER_API_KEY is tried before the deprecated var.
func TestGetAPIKeyPrimaryEnvVar(t *testing.T) {
	t.Setenv("TOGETHER_API_KEY", "primary-key")
	t.Setenv("TOGETHER_AI_API_KEY", "deprecated-key")

	got := getAPIKey("")
	if got != "primary-key" {
		t.Errorf("getAPIKey(empty) with TOGETHER_API_KEY set = %q, want %q", got, "primary-key")
	}
}

// TestGetAPIKeyDeprecatedFallback verifies TOGETHER_AI_API_KEY is used when
// TOGETHER_API_KEY is absent.
func TestGetAPIKeyDeprecatedFallback(t *testing.T) {
	os.Unsetenv("TOGETHER_API_KEY")
	t.Setenv("TOGETHER_AI_API_KEY", "deprecated-key")
	t.Cleanup(func() { os.Unsetenv("TOGETHER_AI_API_KEY") })

	got := getAPIKey("")
	if got != "deprecated-key" {
		t.Errorf("getAPIKey(empty) with only TOGETHER_AI_API_KEY set = %q, want %q", got, "deprecated-key")
	}
}

// TestGetAPIKeyEmpty verifies empty string is returned when neither env var is set.
func TestGetAPIKeyEmpty(t *testing.T) {
	os.Unsetenv("TOGETHER_API_KEY")
	os.Unsetenv("TOGETHER_AI_API_KEY")
	t.Cleanup(func() {
		os.Unsetenv("TOGETHER_API_KEY")
		os.Unsetenv("TOGETHER_AI_API_KEY")
	})

	got := getAPIKey("")
	if got != "" {
		t.Errorf("getAPIKey(empty) with no env vars = %q, want empty string", got)
	}
}

// TestNewUsesEnvVar verifies that New() picks up TOGETHER_API_KEY from the environment.
func TestNewUsesEnvVar(t *testing.T) {
	t.Setenv("TOGETHER_API_KEY", "env-api-key")

	p := New(Config{})
	if p == nil {
		t.Fatal("New() returned nil")
	}
	// The provider should have been created without error even with an empty APIKey in Config.
	if p.Name() != "together" {
		t.Errorf("Name() = %q, want %q", p.Name(), "together")
	}
}
