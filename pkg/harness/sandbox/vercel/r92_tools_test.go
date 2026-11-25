package vercel

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// 3.9.0 Sandbox.create / getOrCreate throw "`runtime` and `image` cannot
// be used together." before getCredentials, so a session create with both
// fails with that TypeError even when no credentials are configured
// (createVercelNetworkSandboxSession and the legacy provider's
// createSession reach it through Sandbox.create / ensureTemplateSnapshot).
func TestR92toolsVercelRuntimeAndImageBeforeCredentials(t *testing.T) {
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	t.Setenv("VERCEL_OIDC_TOKEN_FILE", "")
	const want = "`runtime` and `image` cannot be used together."
	_, err := CreateNetworkSandboxSession(t.Context(), CreateSessionOptions{Runtime: "node24", Image: "img"})
	if err == nil || err.Error() != want {
		t.Fatalf("CreateNetworkSandboxSession err = %v, want %q", err, want)
	}
	legacy := NewLegacyProvider(LegacySettings{Runtime: "node24", Image: "img"})
	_, err = legacy.CreateSession(t.Context(), harness.CreateSandboxSessionOptions{})
	if err == nil || err.Error() != want {
		t.Fatalf("legacy CreateSession err = %v, want %q", err, want)
	}
}
