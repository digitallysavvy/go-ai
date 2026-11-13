package acp

import (
	"encoding/json"
	"testing"
)

// TS acpResumeStateSchema (acp-harness.ts 130-131) declares
// `agentIdentity: z.string()` and acpTurnStartConfigSchema
// (acp-v1-bridge-protocol.ts 166) `configurationFingerprint: z.string()`:
// zod rejects null for both. encoding/json decodes null into a string
// without error, so Go accepted them.
func TestR90toolsLifecycleStateRejectsNullStrings(t *testing.T) {
	h := &acpHarness{}
	if err := h.ValidateLifecycleStateData(json.RawMessage(`{"agentIdentity":null}`)); err == nil {
		t.Error("agentIdentity: null accepted")
	}
	cfg := `{"agentIdentity":"i","turnStartConfig":{"version":1,"configurationFingerprint":null,"prompt":[],"permissionMode":"allow-all"}}`
	if err := h.ValidateLifecycleStateData(json.RawMessage(cfg)); err == nil {
		t.Error("turnStartConfig.configurationFingerprint: null accepted")
	}
	cold := `{"agentIdentity":"i","coldSession":{"version":1,"configurationFingerprint":null,"permissionMode":"allow-all"}}`
	if err := h.ValidateLifecycleStateData(json.RawMessage(cold)); err == nil {
		t.Error("coldSession.configurationFingerprint: null accepted")
	}
	ok := `{"agentIdentity":"i","turnStartConfig":{"version":1,"configurationFingerprint":"f","prompt":[],"permissionMode":"allow-all"}}`
	if err := h.ValidateLifecycleStateData(json.RawMessage(ok)); err != nil {
		t.Errorf("valid state rejected: %v", err)
	}
}
