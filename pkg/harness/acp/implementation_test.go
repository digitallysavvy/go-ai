package acp

import (
	"strings"
	"testing"
)

// Port of harness-acp/src/v1/implementation.test.ts validateACPV1Implementation
// edge cases: invalid environment-variable names (both forwarded/credential
// names and literal `env` keys) and overlapping sensitive/non-secret
// environment keys across forwardEnv/credentialEnv/env.

func simpleTestImplementation() implementation {
	return implementation{
		Source:     Source{Type: SourceNPMSimple, PackageName: "@example/acp-agent", PackageVersion: "1.2.3"},
		Executable: "acp-agent",
		Args:       []string{"stdio"},
		ForwardEnv: []string{"PROVIDER_API_KEY", "SECOND_PROVIDER_API_KEY"},
		Env:        map[string]string{"PROVIDER_BASE_URL": "https://provider.example"},
	}
}

// TS: "rejects overlapping sensitive and non-secret environment keys"
func TestValidateImplementation_RejectsOverlappingForwardEnvAndEnv(t *testing.T) {
	impl := simpleTestImplementation()
	impl.Env = map[string]string{"PROVIDER_API_KEY": "not-secret"}
	err := validateImplementation(impl)
	if err == nil {
		t.Fatal("expected an error for a key configured in both forwardEnv and env")
	}
	if got := err.Error(); !containsAll(got, "forwardEnv", "env") {
		t.Fatalf("error = %q, want it to mention forwardEnv and env", got)
	}
}

// TS: "requires credential environment keys to be distinct from other
// environment settings"
func TestValidateImplementation_RequiresCredentialEnvDistinct(t *testing.T) {
	t.Run("overlaps forwardEnv", func(t *testing.T) {
		impl := simpleTestImplementation()
		impl.CredentialEnv = []string{"PROVIDER_API_KEY"}
		err := validateImplementation(impl)
		if err == nil {
			t.Fatal("expected an error: PROVIDER_API_KEY configured in both forwardEnv and credentialEnv")
		}
		if got := err.Error(); !containsAll(got, "forwardEnv", "credentialEnv") {
			t.Fatalf("error = %q, want it to mention forwardEnv and credentialEnv", got)
		}
	})
	t.Run("overlaps env", func(t *testing.T) {
		impl := simpleTestImplementation()
		impl.CredentialEnv = []string{"PROVIDER_BASE_URL"}
		err := validateImplementation(impl)
		if err == nil {
			t.Fatal("expected an error: PROVIDER_BASE_URL configured in both credentialEnv and env")
		}
		if got := err.Error(); !containsAll(got, "credentialEnv", "env") {
			t.Fatalf("error = %q, want it to mention credentialEnv and env", got)
		}
	})
}

// TS: "rejects invalid forwarded environment-variable names"
func TestValidateImplementation_RejectsInvalidForwardedEnvNames(t *testing.T) {
	t.Run("forwardEnv", func(t *testing.T) {
		impl := simpleTestImplementation()
		impl.ForwardEnv = []string{"not-an-environment-variable"}
		err := validateImplementation(impl)
		if err == nil {
			t.Fatal("expected an error for an invalid forwardEnv name")
		}
		if got := err.Error(); !containsAll(got, "environment variable name is invalid") {
			t.Fatalf("error = %q", got)
		}
	})
	t.Run("credentialEnv", func(t *testing.T) {
		impl := simpleTestImplementation()
		impl.CredentialEnv = []string{"not-an-environment-variable"}
		err := validateImplementation(impl)
		if err == nil {
			t.Fatal("expected an error for an invalid credentialEnv name")
		}
		if got := err.Error(); !containsAll(got, "environment variable name is invalid") {
			t.Fatalf("error = %q", got)
		}
	})
}

// TS: "rejects invalid literal environment-variable names"
func TestValidateImplementation_RejectsInvalidLiteralEnvNames(t *testing.T) {
	impl := simpleTestImplementation()
	impl.Env = map[string]string{"not-an-environment-variable": "value"}
	err := validateImplementation(impl)
	if err == nil {
		t.Fatal("expected an error for an invalid literal env key")
	}
	if got := err.Error(); !containsAll(got, "environment variable name is invalid") {
		t.Fatalf("error = %q", got)
	}
}

// A handful of additional invalid/valid name shapes beyond the single TS
// example, exercising envVarNameRegexp's boundaries directly.
func TestValidateImplementation_EnvNameShapes(t *testing.T) {
	cases := []struct {
		name    string
		envName string
		wantErr bool
	}{
		{"valid upper snake case", "PROVIDER_API_KEY", false},
		{"valid leading underscore", "_PRIVATE", false},
		{"valid lowercase", "provider_api_key", false},
		{"invalid leading digit", "1KEY", true},
		{"invalid hyphen", "PROVIDER-KEY", true},
		{"invalid dot", "PROVIDER.KEY", true},
		{"invalid space", "PROVIDER KEY", true},
		{"invalid empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			impl := implementation{
				Source:     Source{Type: SourceNPMSimple, PackageName: "@example/acp-agent"},
				Executable: "acp-agent",
				ForwardEnv: []string{tc.envName},
			}
			err := validateImplementation(impl)
			if tc.wantErr && err == nil {
				t.Fatalf("forwardEnv %q: expected an error, got none", tc.envName)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("forwardEnv %q: unexpected error: %v", tc.envName, err)
			}
		})
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
