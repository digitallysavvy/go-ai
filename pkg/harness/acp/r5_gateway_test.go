package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

func gatewaySettings(mut func(*Settings)) Settings {
	return testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "gw-token" }
		s.ForwardEnv = nil
		s.Env = map[string]string{}
		s.Auth = harness.AuthEnvironment(map[string]string{
			"AI_GATEWAY_API_KEY": "gw-secret", "AI_GATEWAY_BASE_URL": "https://gw.example/",
		})
		s.ProviderAuthentication = &ProviderAuthentication{GatewayEnv: map[string]any{
			"PROVIDER_API_KEY":  map[string]any{"$source": "gateway-api-key"},
			"PROVIDER_BASE_URL": map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"},
			"PROVIDER_STATIC":   "static",
		}}
		mut(s)
	})
}

func startGateway(t *testing.T, s Settings) (spawnEnv map[string]string) {
	t.Helper()
	srv := bridgetest.NewServer(bridgetest.Options{Token: "gw-token"})
	t.Cleanup(srv.Close)
	sandbox := newFakeSandbox(srv)
	h, err := CreateACP(s)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1", SessionWorkDir: "/vercel/sandbox/w", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	sandbox.mu.Lock()
	defer sandbox.mu.Unlock()
	return sandbox.spawnEnvs[0]
}

func bridgeConfig(t *testing.T, env map[string]string) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal([]byte(env[BridgeConfigurationEnv]), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TS: with credential brokering the host pre-resolves the AI Gateway profile
// into the sandbox process environment (credentials replaced by placeholders)
// and tells the bridge `providerEnvironment: {}`.
func TestDoStart_GatewayProfileResolvedWithBrokering(t *testing.T) {
	env := startGateway(t, gatewaySettings(func(s *Settings) {
		s.CredentialEnv = []string{"UNUSED_KEY"}
		s.CredentialBrokering = func(env, sandboxEnv map[string]string, _ map[string]string) []harness.RequestTransformation {
			if env["PROVIDER_API_KEY"] != "gw-secret" || sandboxEnv["PROVIDER_API_KEY"] == "gw-secret" || sandboxEnv["PROVIDER_API_KEY"] == "" {
				t.Errorf("brokering env=%q sandboxEnv=%q", env["PROVIDER_API_KEY"], sandboxEnv["PROVIDER_API_KEY"])
			}
			return nil
		}
	}))
	blob, _ := json.Marshal(env)
	if strings.Contains(string(blob), "gw-secret") {
		t.Fatalf("spawn env leaked the gateway credential: %s", blob)
	}
	if env["PROVIDER_API_KEY"] == "" || env["PROVIDER_BASE_URL"] != "https://gw.example/v1" || env["PROVIDER_STATIC"] != "static" {
		t.Fatalf("provider env not resolved into the sandbox process: %v", env)
	}
	if _, ok := env["AI_SDK_ACP_GATEWAY_API_KEY"]; ok {
		t.Fatalf("bridge gateway env must be withheld when the provider env is pre-resolved")
	}
	cfg := bridgeConfig(t, env)
	pe, ok := cfg["providerEnvironment"].(map[string]any)
	if !ok || len(pe) != 0 {
		t.Fatalf("providerEnvironment = %#v, want {}", cfg["providerEnvironment"])
	}
}

// TS: credentialForwarding also pre-resolves the gateway profile (and forwards
// the credential through the callback).
func TestDoStart_GatewayProfileResolvedWithForwarding(t *testing.T) {
	env := startGateway(t, gatewaySettings(func(s *Settings) {
		s.CredentialForwarding = func(_ context.Context, opts harness.CredentialForwardingOptions) (string, error) { return opts.Credential, nil }
	}))
	if env["PROVIDER_API_KEY"] != "gw-secret" || env["PROVIDER_BASE_URL"] != "https://gw.example/v1" {
		t.Fatalf("provider env = %v", env)
	}
	cfg := bridgeConfig(t, env)
	if pe, ok := cfg["providerEnvironment"].(map[string]any); !ok || len(pe) != 0 {
		t.Fatalf("providerEnvironment = %#v, want {}", cfg["providerEnvironment"])
	}
}

// Without brokering/forwarding the bridge resolves the profile itself.
func TestDoStart_GatewayProfileResolvedByBridgeWithoutBrokering(t *testing.T) {
	env := startGateway(t, gatewaySettings(func(s *Settings) {}))
	if env["AI_SDK_ACP_GATEWAY_API_KEY"] != "gw-secret" {
		t.Fatalf("env = %v", env)
	}
	if _, ok := bridgeConfig(t, env)["providerEnvironment"]; ok {
		t.Fatal("providerEnvironment must be absent")
	}
}
