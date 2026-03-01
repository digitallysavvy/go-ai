package xai

import "testing"

// TestGetAPIKeyDirectValue verifies an explicit API key takes priority over env.
func TestGetAPIKeyDirectValue(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-key")

	got := getAPIKey("direct-key")
	if got != "direct-key" {
		t.Errorf("getAPIKey(explicit) = %q, want %q", got, "direct-key")
	}
}

// TestGetAPIKeyEnvVar verifies XAI_API_KEY is read from the environment.
func TestGetAPIKeyEnvVar(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-api-key")

	got := getAPIKey("")
	if got != "env-api-key" {
		t.Errorf("getAPIKey(empty) with XAI_API_KEY set = %q, want %q", got, "env-api-key")
	}
}

// TestGetAPIKeyEmpty verifies empty string is returned when no key is configured.
func TestGetAPIKeyEmpty(t *testing.T) {
	// Ensure XAI_API_KEY is not set
	t.Setenv("XAI_API_KEY", "")

	got := getAPIKey("")
	if got != "" {
		t.Errorf("getAPIKey(empty) with no env var = %q, want empty string", got)
	}
}

// TestNewUsesEnvVar verifies that New() picks up XAI_API_KEY from the environment.
func TestNewUsesEnvVar(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-api-key")

	p := New(Config{})
	if p == nil {
		t.Fatal("New() returned nil")
	}
	if p.Name() != "xai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "xai")
	}
}
