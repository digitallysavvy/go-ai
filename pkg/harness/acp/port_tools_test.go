package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// TS 6f9d73fd19 acp-harness.test.ts "passes write-protected paths to the bridge
// configuration".
func TestPortToolsWriteProtectedPathsReachBridgeConfiguration(t *testing.T) {
	srv := newServer(t, "wpp-token", func(*bridgetest.Turn, map[string]any) {})
	sandbox := newFakeSandbox(srv)
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.WriteProtectedPaths = []string{".example", ".example-config/hooks"}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "wpp-session", SessionWorkDir: "/workspace/user-project", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()
	sandbox.mu.Lock()
	spawnEnv := sandbox.spawnEnvs[len(sandbox.spawnEnvs)-1]
	sandbox.mu.Unlock()
	var cfg struct {
		WriteProtectedPaths []string `json:"writeProtectedPaths"`
	}
	if err := json.Unmarshal([]byte(spawnEnv[BridgeConfigurationEnv]), &cfg); err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.WriteProtectedPaths, ",") != ".example,.example-config/hooks" {
		t.Fatalf("writeProtectedPaths = %v", cfg.WriteProtectedPaths)
	}
}

// TS acp-v1-bridge-environment.test.ts "round-trips write-protected paths":
// the key follows nonTextContentTypes, an empty list still travels, nil is
// omitted.
func TestPortToolsBridgeEnvironmentWriteProtectedPaths(t *testing.T) {
	env := createBridgeEnvironment(bridgeConfiguration{WriteProtectedPaths: []string{".github/hooks", ".mcp.json"}})
	want := `{"agent":{"command":"","args":[]},"writeProtectedPaths":[".github/hooks",".mcp.json"]}`
	if got := env[BridgeConfigurationEnv]; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	env = createBridgeEnvironment(bridgeConfiguration{NonTextContentTypes: []NonTextContentType{}, WriteProtectedPaths: []string{}})
	want = `{"agent":{"command":"","args":[]},"nonTextContentTypes":[],"writeProtectedPaths":[]}`
	if got := env[BridgeConfigurationEnv]; got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	if got := createBridgeEnvironment(bridgeConfiguration{})[BridgeConfigurationEnv]; strings.Contains(got, "writeProtectedPaths") {
		t.Fatalf("nil paths serialized: %s", got)
	}
}

// TS acp-v1-settings.test.ts "accepts relative write-protected paths" and
// "rejects write-protected path %j".
func TestPortToolsValidateWriteProtectedPaths(t *testing.T) {
	if _, err := CreateACP(testSettings(func(s *Settings) {
		s.WriteProtectedPaths = []string{".github/hooks", ".mcp.json", ".cursor"}
	})); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"", " ", "/abs", "../x", "a/../b", "./a", "a//b", "a/", "a\x00b"} {
		_, err := CreateACP(testSettings(func(s *Settings) { s.WriteProtectedPaths = []string{entry} }))
		if err == nil || !strings.Contains(err.Error(), "ACP writeProtectedPaths entries must be relative paths inside the working directory") {
			t.Errorf("entry %q: err = %v", entry, err)
		}
	}
}
