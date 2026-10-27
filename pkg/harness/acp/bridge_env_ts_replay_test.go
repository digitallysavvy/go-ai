package acp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

var tsGateway = gatewayValues{APIKey: "gateway-key", BaseURL: "https://gateway.example", ClientAppName: "ai-sdk-harness-acp", ClientAppVersion: "1.2.3"}

// Replays TS profile-values.test.ts.
func TestTSReplay_ProfileValues(t *testing.T) {
	got := resolveProfileValue(map[string]any{"gateway": map[string]any{
		"baseUrl": map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"},
		"headers": map[string]any{
			"Authorization":        map[string]any{"$source": "gateway-authorization"},
			"x-client-app":         map[string]any{"$source": "client-app"},
			"x-client-app-name":    map[string]any{"$source": "client-app-name"},
			"x-client-app-version": map[string]any{"$source": "client-app-version"},
		},
	}}, tsGateway)
	want := map[string]any{"gateway": map[string]any{
		"baseUrl": "https://gateway.example/v1",
		"headers": map[string]any{"Authorization": "Bearer gateway-key", "x-client-app": "ai-sdk-harness-acp/1.2.3", "x-client-app-name": "ai-sdk-harness-acp", "x-client-app-version": "1.2.3"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	g := tsGateway
	g.BaseURL = "https://gateway.example/v1/"
	if got := resolveProfileValue(map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"}, g); got != "https://gateway.example/v1" {
		t.Fatalf("suffix duplicated: %v", got)
	}
	// prefix/suffix
	if got := resolveProfileValue(map[string]any{"$source": "client-app", "prefix": "x-client-app: ", "suffix": "!"}, tsGateway); got != "x-client-app: ai-sdk-harness-acp/1.2.3!" {
		t.Fatalf("prefix/suffix: %v", got)
	}
}

// Replays the launch-environment cases of TS protocol-configuration.test.ts.
func TestTSReplay_LaunchEnvironment(t *testing.T) {
	gw := gatewayValues{APIKey: "gateway-key", BaseURL: "https://gateway.example", ClientAppName: "ai-sdk-harness-acp", ClientAppVersion: "0.0.0-test"}
	cases := []struct {
		name    string
		profile map[string]any
		want    map[string]string
	}{
		{"gateway launch environment", map[string]any{"CODEX_CONFIG": `{"model":"openai/gpt-5.5"}`, "PROVIDER_BASE_URL": map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"}},
			map[string]string{"CODEX_CONFIG": `{"model":"openai/gpt-5.5"}`, "PROVIDER_BASE_URL": "https://gateway.example/v1"}},
		{"structured values are JSON", map[string]any{"CODEX_CONFIG": map[string]any{"model": "openai/gpt-5.5", "model_providers": map[string]any{"ai_gateway": map[string]any{"base_url": map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"}}}}},
			map[string]string{"CODEX_CONFIG": `{"model":"openai/gpt-5.5","model_providers":{"ai_gateway":{"base_url":"https://gateway.example/v1"}}}`}},
		{"client attribution headers", map[string]any{"CODEX_CONFIG": map[string]any{"model_providers": map[string]any{"ai_gateway": map[string]any{"http_headers": map[string]any{
			"User-Agent": map[string]any{"$source": "client-app"}, "x-client-app": map[string]any{"$source": "client-app"}}}}}},
			map[string]string{"CODEX_CONFIG": `{"model_providers":{"ai_gateway":{"http_headers":{"User-Agent":"ai-sdk-harness-acp/0.0.0-test","x-client-app":"ai-sdk-harness-acp/0.0.0-test"}}}}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveLaunchEnvironment(tc.profile, gw)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
	got, err := resolveProviderEnvironment(resolvedProviderAuthentication{Type: "direct"}, tsClientApp)
	if err != nil || len(got) != 0 {
		t.Fatalf("direct launch environment = %v, %v", got, err)
	}
}

// Replays TS acp-v1-bridge-environment.test.ts "round-trips ..." through JSON.
func TestTSReplay_BridgeEnvironmentWireShape(t *testing.T) {
	providerEnv := map[string]string{"AI_GATEWAY_API_KEY": "AI_GATEWAY_API_KEY", "AI_GATEWAY_BASE_URL": "https://gateway.example/v1"}
	env := createBridgeEnvironment(bridgeConfiguration{
		Agent:          agentLaunch{Command: "/bridge/node_modules/.bin/agent", Args: []string{"stdio"}},
		Authentication: &Authentication{MethodID: "api-key", Meta: map[string]any{"apiKey": "direct-secret"}},
		ProviderAuthentication: &bridgeProviderAuth{Type: "ai-gateway", Env: map[string]any{
			"AI_GATEWAY_API_KEY": map[string]any{"$source": "gateway-api-key"}, "CLIENT_APP": map[string]any{"$source": "client-app"}}},
		ProviderEnvironment: &providerEnv,
		SessionMeta:         map[string]any{"opaqueCredential": "session-secret"},
		ClientCapabilities:  map[string]any{"_meta": map[string]any{"parameterizedModelPicker": true}},
	})
	var got map[string]any
	if err := json.Unmarshal([]byte(env[BridgeConfigurationEnv]), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"agent":          map[string]any{"command": "/bridge/node_modules/.bin/agent", "args": []any{"stdio"}},
		"authentication": map[string]any{"methodId": "api-key", "meta": map[string]any{"apiKey": "direct-secret"}},
		"providerAuthentication": map[string]any{"type": "ai-gateway", "env": map[string]any{
			"AI_GATEWAY_API_KEY": map[string]any{"$source": "gateway-api-key"}, "CLIENT_APP": map[string]any{"$source": "client-app"}}},
		"providerEnvironment": map[string]any{"AI_GATEWAY_API_KEY": "AI_GATEWAY_API_KEY", "AI_GATEWAY_BASE_URL": "https://gateway.example/v1"},
		"sessionMeta":         map[string]any{"opaqueCredential": "session-secret"},
		"clientCapabilities":  map[string]any{"_meta": map[string]any{"parameterizedModelPicker": true}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configuration = %v", got)
	}
	// nonTextContentTypes: [] and ['image'] round-trip (a declared empty list stays present only when declared).
	for _, types := range [][]NonTextContentType{{NonTextContentImage}} {
		env := createBridgeEnvironment(bridgeConfiguration{NonTextContentTypes: types})
		var m map[string]any
		_ = json.Unmarshal([]byte(env[BridgeConfigurationEnv]), &m)
		if !reflect.DeepEqual(m["nonTextContentTypes"], []any{"image"}) {
			t.Fatalf("nonTextContentTypes = %v", m["nonTextContentTypes"])
		}
	}
}

// Replays TS acp-v1-bootstrap.test.ts by running the generated script.
func TestTSReplay_InstallScriptRuns(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}
	command := "mkdir -p \"$HOME/.local/share/acp-agent/versions/1.0.0\" \"$HOME/.local/bin\"\n" +
		"printf '#!/usr/bin/env bash\\nexit 0\\n' > \"$HOME/.local/share/acp-agent/versions/1.0.0/acp-agent\"\n" +
		"chmod +x \"$HOME/.local/share/acp-agent/versions/1.0.0/acp-agent\"\n" +
		"ln -s \"$HOME/.local/share/acp-agent/versions/1.0.0/acp-agent\" \"$HOME/.local/bin/acp-agent\"\n" +
		"pwd > \"$HOME/install-working-directory.txt\""
	impl := implementation{Source: Source{Type: SourceInstallCommand, Command: command}, Executable: "acp-agent"}
	tmp := t.TempDir()
	implDir := filepath.Join(tmp, ".harness-bootstrap", "example-acp")
	ambientHome := filepath.Join(tmp, "ambient-home")
	if err := os.MkdirAll(implDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ambientHome, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(implDir, "install.sh")
	if err := os.WriteFile(script, []byte(createInstallScript(impl.Source.Command)), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(), "HOME="+ambientHome)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install script failed: %v\n%s", err, out)
	}
	home := filepath.Join(implDir, "home")
	target, err := filepath.EvalSymlinks(filepath.Join(home, ".local", "bin", "acp-agent"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(home, ".local", "share", "acp-agent", "versions", "1.0.0", "acp-agent"))
	if target != want {
		t.Fatalf("symlink resolves to %s, want %s", target, want)
	}
	wd, _ := os.ReadFile(filepath.Join(home, "install-working-directory.txt"))
	realHome, _ := filepath.EvalSymlinks(home)
	if string(wd) != realHome+"\n" && string(wd) != home+"\n" {
		t.Fatalf("working directory = %q, want %q", wd, home)
	}
	if _, err := os.Stat(filepath.Join(ambientHome, ".local", "bin", "acp-agent")); !os.IsNotExist(err) {
		t.Fatalf("the installer touched the ambient HOME: %v", err)
	}

	// A failing download side of a pipeline fails the installer.
	failing := implementation{Source: Source{Type: SourceInstallCommand, Command: "false | true"}, Executable: "acp-agent"}
	fs := filepath.Join(tmp, "fail.sh")
	_ = os.WriteFile(fs, []byte(createInstallScript(failing.Source.Command)), 0o755)
	if err := exec.Command("bash", fs).Run(); err == nil {
		t.Fatal("the installer must fail when the left side of a pipeline fails")
	}
}
