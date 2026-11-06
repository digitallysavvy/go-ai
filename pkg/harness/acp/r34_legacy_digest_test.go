package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// Golden values computed by the v0.5.2 code (a0f9d3f9) for the same inputs.
const (
	v052Digest     = "5e21de86127a4f8a3f8c6e4c229c57b5178b8865ac7acd6766436ecb407a726b"
	v052DigestAuth = "f5d700224f7224a21dc1bc08e95eeb966edbb30c2e93042d8ff4360adedd6144"
	v052DigestNil  = "567fa26c9150dd1db2bf0d6da8dc8a96dc8a646aaca7cc48d25d0e11523a4b07"
	v052FP         = "8ae8530c7fcffbd19a1fe5ffe1ef74f2294a59810e2b44754366d20e27ec5cf7"
	v052FPMin      = "1f3206e0c5e54128fbae2e2c370fd153908d3ad24637a05de738e99a25f08d05"
)

func v052Inputs() (*Authentication, *providerAuthenticationCompatibility) {
	return &Authentication{MethodID: "m<1>", Meta: map[string]any{"A_B": 1, "AB": 2, "a": 3}},
		&providerAuthenticationCompatibility{Type: "ai-gateway", Mode: "auto", GatewayEnv: map[string]any{"K": "$source"}, CredentialSource: gatewaySourceAPIKey, BaseURL: "https://gw"}
}

func v052StartInput(id authenticationProfileIdentity) createTurnStartConfigInput {
	return createTurnStartConfigInput{
		PermissionMode: harness.PermissionModeAllowAll, AuthenticationProfile: id,
		SessionMeta:           map[string]any{"A_B": 1, "AB": 2, "a<": "&"},
		InstructionMapping:    &InstructionMapping{Type: InstructionMappingSessionMeta, Path: []string{"a", "b"}},
		OutputSchemaMapping:   &OutputSchemaMapping{Path: []string{"o"}},
		ModelMapping:          ModelMapping{Type: ModelMappingSessionModel, Path: "p"},
		BuiltinTools:          []BuiltinToolMapping{{ToolName: "t", NativeName: "n"}},
		PermissionModeMapping: &PermissionModeMapping{AllowReads: &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "x"}, AllowAll: &PermissionModeTarget{Type: PermissionTargetSessionConfigOption, ConfigID: "c", Value: true}},
		MCPServers:            map[string]any{"A_B": 1, "AB": 2},
	}
}

func TestLegacyV052DigestAndFingerprintMatchGolden(t *testing.T) {
	auth, compat := v052Inputs()
	id := createAuthenticationProfileIdentity(auth, compat)
	if id.Digest == v052Digest {
		t.Fatal("new digest must use the TS order, not the v0.5.2 format")
	}
	if id.legacyDigest != v052Digest || !id.matchesDigest(v052Digest) || !id.matchesDigest(id.Digest) || id.matchesDigest("x") {
		t.Fatalf("legacy digest %s", id.legacyDigest)
	}
	if got := createAuthenticationProfileIdentity(nil, nil).legacyDigest; got != v052DigestNil {
		t.Fatalf("nil legacy digest %s", got)
	}
	// The v0.5.2 fingerprint covered the v0.5.2 digest.
	if got := legacyConfigurationFingerprint(v052StartInput(id)); got != v052FP {
		t.Fatalf("legacy fingerprint %s", got)
	}
	if got := legacyConfigurationFingerprint(createTurnStartConfigInput{PermissionMode: harness.PermissionModeAllowAll, AuthenticationProfile: createAuthenticationProfileIdentity(nil, nil)}); got != v052FPMin {
		t.Fatalf("minimal legacy fingerprint %s", got)
	}
	if createTurnStartConfig(v052StartInput(id)).ConfigurationFingerprint == v052FP {
		t.Fatal("new fingerprint must use the TS format")
	}
}

func TestValidateAcceptsV052PersistedFingerprints(t *testing.T) {
	auth, compat := v052Inputs()
	id := createAuthenticationProfileIdentity(auth, compat)
	in := v052StartInput(id)
	persisted := createTurnStartConfig(in)
	persisted.ConfigurationFingerprint = v052FP

	err := validateTurnStartConfig(persisted, validateTurnStartConfigInput{
		AuthenticationProfile: id, SessionMeta: in.SessionMeta, InstructionMapping: in.InstructionMapping,
		OutputSchemaMapping: in.OutputSchemaMapping, ModelMapping: in.ModelMapping, BuiltinTools: in.BuiltinTools,
		PermissionModeMapping: in.PermissionModeMapping, MCPServers: in.MCPServers,
	})
	// The golden fingerprint was made with Model unset and a prompt-less
	// config, matching the persisted copy above.
	if err != nil {
		t.Fatalf("v0.5.2 turn start fingerprint rejected: %v", err)
	}
	persisted.ConfigurationFingerprint = "deadbeef"
	if err := validateTurnStartConfig(persisted, validateTurnStartConfigInput{
		AuthenticationProfile: id, SessionMeta: in.SessionMeta, InstructionMapping: in.InstructionMapping,
		OutputSchemaMapping: in.OutputSchemaMapping, ModelMapping: in.ModelMapping, BuiltinTools: in.BuiltinTools,
		PermissionModeMapping: in.PermissionModeMapping, MCPServers: in.MCPServers,
	}); err == nil {
		t.Fatal("unknown fingerprint accepted")
	}

	cold := createColdSessionState(createTurnStartConfig(in))
	cold.ConfigurationFingerprint = v052FP
	if _, err := validateColdSessionConfiguration(cold, validateColdSessionConfigurationInput{
		PermissionMode: in.PermissionMode, AuthenticationProfile: id, SessionMeta: in.SessionMeta,
		InstructionMapping: in.InstructionMapping, OutputSchemaMapping: in.OutputSchemaMapping,
		ModelMapping: in.ModelMapping, BuiltinTools: in.BuiltinTools, PermissionModeMapping: in.PermissionModeMapping, MCPServers: in.MCPServers,
	}); err != nil {
		t.Fatalf("v0.5.2 cold session fingerprint rejected: %v", err)
	}
}

// A lifecycle state persisted by v0.5.2 carries the v0.5.2-format digest and
// must still start (and a different digest must still be rejected).
func TestDoStart_ResumeAcceptsV052AuthenticationDigest(t *testing.T) {
	auth := &Authentication{MethodID: "m", Meta: map[string]any{"A_B": 1, "AB": 2, "a": 3}}
	srv := newServer(t, "tok", func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" }; s.Authentication = auth }))
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: sandbox})
	if err != nil {
		t.Fatal(err)
	}
	state, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	withDigest := func(digest string) *harness.ResumeSessionState {
		var data map[string]any
		_ = json.Unmarshal(state.Data, &data)
		data["authenticationProfile"].(map[string]any)["digest"] = digest
		s, _ := harness.NewResumeSessionState(state.HarnessID, data)
		return s
	}
	if v052DigestAuth == createAuthenticationProfileIdentity(auth, nil).Digest {
		t.Fatal("fixture digest must differ from the TS-format digest")
	}
	resumed, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: sandbox, ResumeFrom: withDigest(v052DigestAuth)})
	if err != nil {
		t.Fatalf("v0.5.2 digest rejected: %v", err)
	}
	t.Cleanup(func() { _ = resumed.DoDestroy(context.Background()) })
	_, err = h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SessionWorkDir: "/w", SandboxSession: sandbox, ResumeFrom: withDigest("0000")})
	if err == nil || !strings.Contains(err.Error(), "incompatible with the configured authentication profile") {
		t.Fatalf("err = %v", err)
	}
}
