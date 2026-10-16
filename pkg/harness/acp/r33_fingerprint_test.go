package acp

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// TS orders the keys of the turn start configuration fingerprint and the
// authentication profile digest by UTF-16 code unit (`left < right`); only the
// implementation identity uses localeCompare.
func TestStableStringifyCodeUnitOrdersByCodeUnit(t *testing.T) {
	v := map[string]any{"A_B": 1, "AB": 2, "a": 3, "B": 4}
	if got, want := stableStringifyCodeUnit(v), `{"AB":2,"A_B":1,"B":4,"a":3}`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if got, want := stableStringify(v), `{"a":3,"A_B":1,"AB":2,"B":4}`; got != want {
		t.Fatalf("identity ordering changed: %s", got)
	}
}

func TestTurnStartConfigFingerprintUsesCodeUnitOrder(t *testing.T) {
	in := createTurnStartConfigInput{
		PermissionMode: harness.PermissionModeAllowAll,
		MCPServers:     map[string]any{"A_B": 1, "AB": 2},
	}
	cfg := createTurnStartConfig(in)
	payload := map[string]any{
		"authenticationProfile": in.AuthenticationProfile,
		"sessionMeta":           in.SessionMeta,
		"modelMapping":          in.ModelMapping,
		"builtinTools":          in.BuiltinTools,
		"permissionModeMapping": in.PermissionModeMapping,
		"mcpServers":            in.MCPServers,
	}
	sum := sha256.Sum256([]byte(stableStringifyCodeUnit(payload)))
	if cfg.ConfigurationFingerprint != hex.EncodeToString(sum[:]) {
		t.Fatal("fingerprint is not over code-unit ordered keys")
	}
}

func TestAuthenticationProfileDigestUsesCodeUnitOrder(t *testing.T) {
	auth := &Authentication{MethodID: "m", Meta: map[string]any{"A_B": 1, "AB": 2, "a": 3}}
	id := createAuthenticationProfileIdentity(auth, nil)
	payload := map[string]any{"authentication": auth, "providerAuthentication": nil}
	sum := sha256.Sum256([]byte(stableStringifyCodeUnit(payload)))
	if id.Digest != hex.EncodeToString(sum[:]) {
		t.Fatal("authentication profile digest is not over code-unit ordered keys")
	}
}
