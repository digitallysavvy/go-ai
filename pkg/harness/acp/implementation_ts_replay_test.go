package acp

import (
	"reflect"
	"strings"
	"testing"
)

const tsPackageJSON = `{
  "name": "locked-acp-agent",
  "private": true,
  "dependencies": {
    "@example/acp-agent": "1.2.3"
  }
}
`

const tsPnpmLock = `lockfileVersion: '9.0'
importers:
  .:
    dependencies:
      '@example/acp-agent':
        specifier: 1.2.3
        version: 1.2.3
`

func tsSimple() implementation {
	return implementation{Source: Source{Type: SourceNPMLocked, PackageJSON: tsPackageJSON, PnpmLockYAML: tsPnpmLock},
		Executable: "acp-agent", Args: []string{"stdio"}, ForwardEnv: []string{"PROVIDER_API_KEY", "SECOND_PROVIDER_API_KEY"},
		Env: map[string]string{"PROVIDER_BASE_URL": "https://provider.example"}}
}

func tsLocked() implementation { return tsSimple() }

func tsInstallCommand() implementation {
	return implementation{Source: Source{Type: SourceInstallCommand, Command: "curl https://example.com/install -fsS | bash"},
		Executable: "acp-agent", Args: []string{"stdio"}, CredentialEnv: []string{"PROVIDER_API_KEY"}}
}

func tsIdentity(mut func(*agentIdentityInput)) string {
	in := agentIdentityInput{HarnessID: "example-acp", Implementation: tsSimple(), ClientApp: ClientApp{Name: "ai-sdk-harness-acp", Version: "0.0.0-test"},
		ModelMapping: ModelMapping{Type: ModelMappingSessionConfigOption, Path: "model"}}
	if mut != nil {
		mut(&in)
	}
	return createAgentIdentity(in)
}

// Replays acp-v1-bootstrap.test.ts: launch resolution and the install script.
func TestTSReplay_AgentLaunch(t *testing.T) {
	locked := tsSimple()
	got := resolveAgentLaunch("/home/agent/.harness-bootstrap/example-acp", locked)
	want := agentLaunch{Command: "/home/agent/.harness-bootstrap/example-acp/node_modules/.bin/acp-agent", Args: []string{"stdio"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("locked launch = %+v, want %+v", got, want)
	}
	ic := implementation{Source: Source{Type: SourceInstallCommand, Command: "install"}, Executable: "agent"}
	got = resolveAgentLaunch("/home/agent/.harness-bootstrap/cursor-acp", ic)
	want = agentLaunch{Command: "/home/agent/.harness-bootstrap/cursor-acp/home/.local/bin/agent", Args: []string{}, Home: "/home/agent/.harness-bootstrap/cursor-acp/home"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("install-command launch = %+v, want %+v", got, want)
	}
	script := createInstallScript("curl https://example.com/install -fsS | bash")
	for _, want := range []string{"#!/usr/bin/env bash\nset -euo pipefail\n", `ACP_BOOTSTRAP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"`, `export HOME="$ACP_BOOTSTRAP_DIR/home"`, `export PATH="$HOME/.local/bin:$PATH"`, `cd "$HOME"`, "curl https://example.com/install -fsS | bash\n"} {
		if !strings.Contains(script, want) {
			t.Fatalf("install script lacks %q: %s", want, script)
		}
	}
}

// The deprecated npm-simple source (removed from TS) keeps working as an
// install command into the agent's private home.
func TestNPMSimpleSourceIsAnInstallCommandShim(t *testing.T) {
	impl := implementation{Source: Source{Type: SourceNPMSimple, PackageName: "@example/acp-agent", PackageVersion: "1.2.3"}, Executable: "acp-agent"}
	if err := validateImplementation(impl); err != nil {
		t.Fatal(err)
	}
	src := effectiveSource(impl.Source)
	if src.Type != SourceInstallCommand || src.Command != `npm install --global --prefix "$HOME/.local" '@example/acp-agent@1.2.3'` {
		t.Fatalf("effective source = %+v", src)
	}
	impl.Source.PackageVersion = ""
	if got := effectiveSource(impl.Source).Command; !strings.HasSuffix(got, "'@example/acp-agent@latest'") {
		t.Fatalf("command = %s", got)
	}
	impl.Source.PackageVersion = "^1.2.3"
	if err := validateImplementation(impl); err == nil || !strings.Contains(err.Error(), "must be an exact semantic version") {
		t.Fatal(err)
	}
	if got := resolveAgentLaunch("/b", implementation{Source: Source{Type: SourceNPMSimple, PackageName: "p"}, Executable: "p"}); got.Home != "/b/home" {
		t.Fatalf("launch = %+v", got)
	}
}

func TestTSReplay_ImplementationValidation(t *testing.T) {
	bad := tsInstallCommand()
	bad.Source.Command = "  \n"
	if err := validateImplementation(bad); err == nil || !strings.Contains(err.Error(), "source.command must not be empty") {
		t.Fatal(err)
	}
	if err := validateImplementation(tsLocked()); err != nil {
		t.Fatal(err)
	}
	if err := validateImplementation(tsInstallCommand()); err != nil {
		t.Fatal(err)
	}
}

func TestTSReplay_ResolveImplementationEnvironment(t *testing.T) {
	base := tsSimple()
	split := tsSimple()
	split.ForwardEnv = []string{"SECOND_PROVIDER_API_KEY"}
	split.CredentialEnv = []string{"PROVIDER_API_KEY"}
	cases := []struct {
		name         string
		impl         implementation
		env, credEnv map[string]string
		want         map[string]string
	}{
		{"forwards configured variables only", base, map[string]string{"PROVIDER_API_KEY": "provider-secret", "SECOND_PROVIDER_API_KEY": "second-provider-secret", "UNRELATED_SECRET": "unrelated-secret"}, nil,
			map[string]string{"PROVIDER_API_KEY": "provider-secret", "SECOND_PROVIDER_API_KEY": "second-provider-secret", "PROVIDER_BASE_URL": "https://provider.example"}},
		{"empty env", base, map[string]string{}, nil, map[string]string{"PROVIDER_BASE_URL": "https://provider.example"}},
		{"partial env", base, map[string]string{"SECOND_PROVIDER_API_KEY": "second-provider-secret"}, nil,
			map[string]string{"SECOND_PROVIDER_API_KEY": "second-provider-secret", "PROVIDER_BASE_URL": "https://provider.example"}},
		{"credential env is never read from the ambient env", split, map[string]string{"PROVIDER_API_KEY": "provider-secret", "SECOND_PROVIDER_API_KEY": "second-provider-secret"}, nil,
			map[string]string{"SECOND_PROVIDER_API_KEY": "second-provider-secret", "PROVIDER_BASE_URL": "https://provider.example"}},
		{"programmatic credential env wins", split, map[string]string{"PROVIDER_API_KEY": "ambient-provider-secret", "SECOND_PROVIDER_API_KEY": "forwarded-value"},
			map[string]string{"PROVIDER_API_KEY": "programmatic-provider-secret"},
			map[string]string{"PROVIDER_API_KEY": "programmatic-provider-secret", "SECOND_PROVIDER_API_KEY": "forwarded-value", "PROVIDER_BASE_URL": "https://provider.example"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveImplementationEnvironment(tc.impl, tc.env, tc.credEnv); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// Replays 'identifies every non-secret acquisition and launch input' and the
// related identity cases.
func TestTSReplay_ImplementationIdentity(t *testing.T) {
	base := tsIdentity(nil)
	same := []struct {
		name string
		mut  func(*agentIdentityInput)
	}{
		{"stable", nil},
		{"forwardEnv order", func(i *agentIdentityInput) {
			i.Implementation.ForwardEnv = []string{"SECOND_PROVIDER_API_KEY", "PROVIDER_API_KEY"}
		}},
		{"empty nonTextContentTypes", func(i *agentIdentityInput) { i.NonTextContentTypes = []NonTextContentType{} }},
	}
	for _, tc := range same {
		if got := tsIdentity(tc.mut); got != base {
			t.Errorf("%s changed the identity", tc.name)
		}
	}
	differ := []struct {
		name string
		mut  func(*agentIdentityInput)
	}{
		{"credentialEnv split", func(i *agentIdentityInput) {
			i.Implementation.ForwardEnv = []string{"SECOND_PROVIDER_API_KEY"}
			i.Implementation.CredentialEnv = []string{"PROVIDER_API_KEY"}
		}},
		{"other forwardEnv", func(i *agentIdentityInput) { i.Implementation.ForwardEnv = []string{"OTHER_PROVIDER_API_KEY"} }},
		{"install command", func(i *agentIdentityInput) { i.Implementation = tsInstallCommand() }},
		{"executable", func(i *agentIdentityInput) { i.Implementation.Executable = "other-agent" }},
		{"args", func(i *agentIdentityInput) { i.Implementation.Args = []string{"other"} }},
		{"third forwarded", func(i *agentIdentityInput) {
			i.Implementation.ForwardEnv = []string{"PROVIDER_API_KEY", "SECOND_PROVIDER_API_KEY", "THIRD_SECRET"}
		}},
		{"env value", func(i *agentIdentityInput) {
			i.Implementation.Env = map[string]string{"PROVIDER_BASE_URL": "https://other.example"}
		}},
		{"harness id", func(i *agentIdentityInput) { i.HarnessID = "other-acp" }},
		{"client app", func(i *agentIdentityInput) { i.ClientApp = ClientApp{Name: "custom-client", Version: "4.5.6"} }},
		{"client capabilities", func(i *agentIdentityInput) {
			i.ClientCapabilities = map[string]any{"_meta": map[string]any{"parameterizedModelPicker": true}}
		}},
		{"model mapping", func(i *agentIdentityInput) {
			i.ModelMapping = ModelMapping{Type: ModelMappingSessionModel, Path: "modelId"}
		}},
		{"permission mapping", func(i *agentIdentityInput) {
			i.PermissionModeMapping = &PermissionModeMapping{
				AllowReads: &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "read-only"},
				AllowEdits: &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "agent"},
				AllowAll:   &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "full-access"},
			}
		}},
		{"image output", func(i *agentIdentityInput) {
			i.NonTextContentTypes = []NonTextContentType{NonTextContentImage}
		}},
	}
	for _, tc := range differ {
		if got := tsIdentity(tc.mut); got == base {
			t.Errorf("%s did not change the identity", tc.name)
		}
	}
	if tsIdentity(func(i *agentIdentityInput) {
		i.NonTextContentTypes = []NonTextContentType{NonTextContentImage, NonTextContentImage}
	}) !=
		tsIdentity(func(i *agentIdentityInput) {
			i.NonTextContentTypes = []NonTextContentType{NonTextContentImage}
		}) {
		t.Error("duplicate image declarations must not change the identity")
	}
	// Every locked artifact is identified independently.
	locked := tsIdentity(func(i *agentIdentityInput) { i.Implementation = tsLocked() })
	for name, mut := range map[string]func(*Source){
		"packageJson": func(s *Source) { s.PackageJSON = strings.Replace(tsPackageJSON, "locked-acp-agent", "other-agent", 1) },
		"lock":        func(s *Source) { s.PnpmLockYAML += "\n# changed\n" },
		"workspace":   func(s *Source) { s.PnpmWorkspaceYAML = "allowBuilds:\n  '@example/acp-agent@1.2.3': true\n" },
	} {
		if tsIdentity(func(i *agentIdentityInput) { i.Implementation = tsLocked(); mut(&i.Implementation.Source) }) == locked {
			t.Errorf("locked %s did not change the identity", name)
		}
	}
	// Provider routing is identified without credentials.
	gw := &providerAuthenticationCompatibility{Type: "ai-gateway", Mode: "ai-gateway", GatewayEnv: map[string]any{"PROVIDER_API_KEY": map[string]any{"$source": "gateway-api-key"}},
		CredentialSource: gatewaySourceAPIKey, BaseURL: "https://gateway.example"}
	gwID := tsIdentity(func(i *agentIdentityInput) { i.ProviderAuthentication = gw })
	other := *gw
	other.CredentialSource = gatewaySourceOIDCToken
	other2 := *gw
	other2.BaseURL = "https://other-gateway.example"
	for _, c := range []*providerAuthenticationCompatibility{&other, &other2} {
		c := c
		if tsIdentity(func(i *agentIdentityInput) { i.ProviderAuthentication = c }) == gwID {
			t.Errorf("provider routing change %+v did not change the identity", c)
		}
	}
}

// The expected digests below come from running the TS `createAgentIdentity`
// and `createACPAuthenticationProfileIdentity` algorithms (acp-v1-lifecycle.ts,
// acp-auth.ts) in Node on the same inputs, so an ACP lifecycle state can be
// compared across hosts.
func TestTSReplay_IdentityDigestsMatchTS(t *testing.T) {
	mm := ModelMapping{Type: ModelMappingSessionConfigOption, Path: "model"}
	app := ClientApp{Name: "ai-sdk-harness-acp", Version: "0.0.0-test"}
	locked := tsSimple()
	locked.Source = Source{Type: SourceNPMLocked, PackageJSON: "{\"a\":\"<b>&c\"}\n", PnpmLockYAML: "lock: 'x'\n"}
	withWorkspace := tsSimple()
	withWorkspace.Source.PnpmWorkspaceYAML = "allowBuilds:\n  '@example/acp-agent@1.2.3': true\n"
	install := tsInstallCommand()
	install.Source.Command = `curl https://example.com/i -fsS | bash && echo "<done>"`
	gw := &providerAuthenticationCompatibility{Type: "ai-gateway", Mode: "ai-gateway", GatewayEnv: map[string]any{"PROVIDER_API_KEY": map[string]any{"$source": "gateway-api-key"}},
		CredentialSource: gatewaySourceAPIKey, BaseURL: "https://gateway.example"}
	pm := &PermissionModeMapping{
		AllowReads: &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "read-only"},
		AllowAll:   &PermissionModeTarget{Type: PermissionTargetSessionConfigOption, ConfigID: "perm", Value: true},
	}
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"locked", createAgentIdentity(agentIdentityInput{HarnessID: "example-acp", Implementation: tsSimple(), ClientApp: app, ModelMapping: mm}), "b3b5c6143f80f29aa8639c9c078631fde8a63c779bc60bbf2b0e2853c68e2ddb"},
		{"locked with html characters", createAgentIdentity(agentIdentityInput{HarnessID: "example-acp", Implementation: locked, ClientApp: app, ModelMapping: mm}), "408bde7167bacc3a69c8abd977a2d2bb88d2b78b93eba32c98931ba0a826b39b"},
		{"locked with workspace", createAgentIdentity(agentIdentityInput{HarnessID: "example-acp", Implementation: withWorkspace, ClientApp: app, ModelMapping: mm}), "d4b2470930e9d9f8560bd43dfac6c0662a9da7742c529947b0909846f51e4055"},
		{"install command", createAgentIdentity(agentIdentityInput{HarnessID: "example-acp", Implementation: install, ClientApp: app, ModelMapping: mm, NonTextContentTypes: []NonTextContentType{NonTextContentImage, NonTextContentImage}}), "767651b769ccfabed25532cb7f89ba3b105254e501363f3a9d4b60ecc3592493"},
		{"gateway", createAgentIdentity(agentIdentityInput{HarnessID: "example-acp", Implementation: tsSimple(), ClientApp: app, ClientCapabilities: map[string]any{"_meta": map[string]any{"x": true}},
			ModelMapping: mm, ProviderAuthentication: gw, PermissionModeMapping: pm}), "8ee72d6c36698ff94609bf67d2e033ec83d8ac7b312516379032309bc2fa2350"},
		{"profile direct", createAuthenticationProfileIdentity(&Authentication{MethodID: "api-key"}, &providerAuthenticationCompatibility{Type: "direct", Mode: "direct"}).Digest, "1b500f6fed9f68df7b69622a46c920a743f245b53d693b06d4c79f0af491487a"},
		{"profile gateway", createAuthenticationProfileIdentity(&Authentication{MethodID: "api-key", Meta: map[string]any{"k": "v"}, ClientCapabilities: map[string]any{"c": float64(1)}}, gw).Digest, "dd766f3be9169111993c42e7d468974cbcb0b3d6feeaa053a45769caea2e1f49"},
		{"profile none", createAuthenticationProfileIdentity(nil, nil).Digest, "567fa26c9150dd1db2bf0d6da8dc8a96dc8a646aaca7cc48d25d0e11523a4b07"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("digest = %s, TS = %s", tc.got, tc.want)
			}
		})
	}
}

// TS sortValue orders object keys with String.prototype.localeCompare (ICU
// root collation), not by bytes, and JSON.stringify leaves U+2028 literal.
// Expected output produced by running the TS algorithm in Node.
func TestTSReplay_StableStringifyUsesLocaleKeyOrder(t *testing.T) {
	got := stableStringify(map[string]any{
		"AB": 1, "A_B": 2, "a": 3, "B": 4,
		"_m": map[string]any{"z": " <>&\\u2028", "Y": []any{map[string]any{"b": 1, "a": nil}}},
	})
	want := "{\"_m\":{\"Y\":[{\"a\":null,\"b\":1}],\"z\":\" <>&\\\\u2028\"},\"a\":3,\"A_B\":2,\"AB\":1,\"B\":4}"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
