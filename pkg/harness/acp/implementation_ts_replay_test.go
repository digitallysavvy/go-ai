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
	return implementation{Source: Source{Type: SourceNPMSimple, PackageName: "@example/acp-agent", PackageVersion: "1.2.3"},
		Executable: "acp-agent", Args: []string{"stdio"}, ForwardEnv: []string{"PROVIDER_API_KEY", "SECOND_PROVIDER_API_KEY"},
		Env: map[string]string{"PROVIDER_BASE_URL": "https://provider.example"}}
}

func tsUnpinned() implementation {
	i := tsSimple()
	i.Source = Source{Type: SourceNPMSimple, PackageName: "@example/acp-agent"}
	return i
}

func tsLocked() implementation {
	i := tsSimple()
	i.Source = Source{Type: SourceNPMLocked, PackageJSON: tsPackageJSON, PnpmLockYAML: tsPnpmLock}
	return i
}

func tsInstallCommand() implementation {
	return implementation{Source: Source{Type: SourceInstallCommand, Command: "curl https://example.com/install -fsS | bash"},
		Executable: "acp-agent", Args: []string{"stdio"}, CredentialEnv: []string{"PROVIDER_API_KEY"}}
}

func tsIdentity(mut func(*implementationIdentityInput)) string {
	in := implementationIdentityInput{HarnessID: "example-acp", Implementation: tsSimple(), ClientApp: ClientApp{Name: "ai-sdk-harness-acp", Version: "0.0.0-test"},
		ModelMapping: ModelMapping{Type: ModelMappingSessionConfigOption, Path: "model"}}
	if mut != nil {
		mut(&in)
	}
	return createImplementationIdentity(in)
}

func TestTSReplay_ImplementationManifests(t *testing.T) {
	want := "{\n  \"name\": \"harness-acp-implementation\",\n  \"version\": \"0.0.0\",\n  \"private\": true,\n  \"type\": \"module\",\n  \"dependencies\": {\n    \"@example/acp-agent\": \"%s\"\n  }\n}\n"
	if got := createImplementationManifest(tsSimple()); got == nil || *got != strings.Replace(want, "%s", "1.2.3", 1) {
		t.Fatalf("simple manifest = %v", got)
	}
	if got := createImplementationManifest(tsUnpinned()); got == nil || *got != strings.Replace(want, "%s", "latest", 1) {
		t.Fatalf("unpinned manifest = %v", got)
	}
	if err := validateImplementation(tsUnpinned()); err != nil {
		t.Fatal(err)
	}
	if got := createImplementationInstallCommand("/tmp/harness/example/implementation", "/tmp/harness/example/.pnpm-store", tsSimple()); got != "pnpm --dir /tmp/harness/example/implementation install --prod --store-dir /tmp/harness/example/.pnpm-store" {
		t.Fatal(got)
	}
	if got := createImplementationInstallCommand("/tmp/harness/example/implementation", "/tmp/harness/example/.pnpm-store", tsLocked()); got != "pnpm --dir /tmp/harness/example/implementation install --frozen-lockfile --prod --store-dir /tmp/harness/example/.pnpm-store" {
		t.Fatal(got)
	}
	if m := createImplementationManifest(tsLocked()); m == nil || *m != tsPackageJSON {
		t.Fatalf("locked manifest = %v", m)
	}
	if l := getImplementationLockfile(tsLocked()); l == nil || *l != tsPnpmLock {
		t.Fatalf("locked lockfile = %v", l)
	}
	if getImplementationLockfile(tsSimple()) != nil {
		t.Fatal("simple installs have no lockfile")
	}
	ic := tsInstallCommand()
	if createImplementationManifest(ic) != nil || getImplementationLockfile(ic) != nil {
		t.Fatal("install-command sources have neither manifest nor lockfile")
	}
	if got := createImplementationInstallCommand("/tmp/x", "/tmp/s", ic); got != "bash implementation/install.sh" {
		t.Fatal(got)
	}
	script := getImplementationInstallScript(ic)
	for _, want := range []string{"set -euo pipefail", `ACP_INSTALL_HOME="$ACP_IMPLEMENTATION_DIR/home"`, `export PATH="$ACP_INSTALL_HOME/.local/bin:$PATH"`, "curl https://example.com/install -fsS | bash"} {
		if script == nil || !strings.Contains(*script, want) {
			t.Fatalf("install script lacks %q: %v", want, script)
		}
	}
}

func TestTSReplay_ImplementationValidation(t *testing.T) {
	bad := tsInstallCommand()
	bad.Source.Command = "  \n"
	if err := validateImplementation(bad); err == nil || !strings.Contains(err.Error(), "source.command must not be empty") {
		t.Fatal(err)
	}
	s := tsSimple()
	s.Source.PackageVersion = "^1.2.3"
	if err := validateImplementation(s); err == nil || !strings.Contains(err.Error(), "must be an exact semantic version") {
		t.Fatal(err)
	}
	if err := validateImplementation(tsLocked()); err != nil {
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
		{"credential env from the same source", split, map[string]string{"PROVIDER_API_KEY": "provider-secret", "SECOND_PROVIDER_API_KEY": "second-provider-secret"}, nil,
			map[string]string{"PROVIDER_API_KEY": "provider-secret", "SECOND_PROVIDER_API_KEY": "second-provider-secret", "PROVIDER_BASE_URL": "https://provider.example"}},
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

func TestTSReplay_ImplementationDescriptor(t *testing.T) {
	d := createImplementationDescriptor(tsSimple())
	for _, want := range []string{`"PROVIDER_API_KEY"`, `"SECOND_PROVIDER_API_KEY"`, `"PROVIDER_BASE_URL"`, `"executablePath": "node_modules/.bin/acp-agent"`} {
		if !strings.Contains(d, want) {
			t.Errorf("descriptor lacks %s: %s", want, d)
		}
	}
	if strings.Contains(d, `"implementationIdentity"`) || strings.Contains(d, "https://provider.example") {
		t.Errorf("descriptor leaks configuration: %s", d)
	}
	ic := createImplementationDescriptor(tsInstallCommand())
	if !strings.Contains(ic, `"executablePath": "home/.local/bin/acp-agent"`) || !strings.Contains(ic, `"privateHome": true`) {
		t.Errorf("install-command descriptor = %s", ic)
	}
}

// Replays 'identifies every non-secret acquisition and launch input' and the
// related identity cases.
func TestTSReplay_ImplementationIdentity(t *testing.T) {
	base := tsIdentity(nil)
	same := []struct {
		name string
		mut  func(*implementationIdentityInput)
	}{
		{"stable", nil},
		{"forwardEnv order", func(i *implementationIdentityInput) {
			i.Implementation.ForwardEnv = []string{"SECOND_PROVIDER_API_KEY", "PROVIDER_API_KEY"}
		}},
		{"empty nonTextContentTypes", func(i *implementationIdentityInput) { i.NonTextContentTypes = []NonTextContentType{} }},
	}
	for _, tc := range same {
		if got := tsIdentity(tc.mut); got != base {
			t.Errorf("%s changed the identity", tc.name)
		}
	}
	differ := []struct {
		name string
		mut  func(*implementationIdentityInput)
	}{
		{"credentialEnv split", func(i *implementationIdentityInput) {
			i.Implementation.ForwardEnv = []string{"SECOND_PROVIDER_API_KEY"}
			i.Implementation.CredentialEnv = []string{"PROVIDER_API_KEY"}
		}},
		{"other forwardEnv", func(i *implementationIdentityInput) { i.Implementation.ForwardEnv = []string{"OTHER_PROVIDER_API_KEY"} }},
		{"locked", func(i *implementationIdentityInput) { i.Implementation = tsLocked() }},
		{"install command", func(i *implementationIdentityInput) { i.Implementation = tsInstallCommand() }},
		{"package name", func(i *implementationIdentityInput) { i.Implementation.Source.PackageName = "@example/other-agent" }},
		{"package version", func(i *implementationIdentityInput) { i.Implementation.Source.PackageVersion = "1.2.4" }},
		{"executable", func(i *implementationIdentityInput) { i.Implementation.Executable = "other-agent" }},
		{"args", func(i *implementationIdentityInput) { i.Implementation.Args = []string{"other"} }},
		{"third forwarded", func(i *implementationIdentityInput) {
			i.Implementation.ForwardEnv = []string{"PROVIDER_API_KEY", "SECOND_PROVIDER_API_KEY", "THIRD_SECRET"}
		}},
		{"env value", func(i *implementationIdentityInput) {
			i.Implementation.Env = map[string]string{"PROVIDER_BASE_URL": "https://other.example"}
		}},
		{"harness id", func(i *implementationIdentityInput) { i.HarnessID = "other-acp" }},
		{"client app", func(i *implementationIdentityInput) { i.ClientApp = ClientApp{Name: "custom-client", Version: "4.5.6"} }},
		{"client capabilities", func(i *implementationIdentityInput) {
			i.ClientCapabilities = map[string]any{"_meta": map[string]any{"parameterizedModelPicker": true}}
		}},
		{"model mapping", func(i *implementationIdentityInput) {
			i.ModelMapping = ModelMapping{Type: ModelMappingSessionModel, Path: "modelId"}
		}},
		{"permission mapping", func(i *implementationIdentityInput) {
			i.PermissionModeMapping = &PermissionModeMapping{
				AllowReads: &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "read-only"},
				AllowEdits: &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "agent"},
				AllowAll:   &PermissionModeTarget{Type: PermissionTargetSessionMode, ModeID: "full-access"},
			}
		}},
		{"image output", func(i *implementationIdentityInput) {
			i.NonTextContentTypes = []NonTextContentType{NonTextContentImage}
		}},
	}
	for _, tc := range differ {
		if got := tsIdentity(tc.mut); got == base {
			t.Errorf("%s did not change the identity", tc.name)
		}
	}
	if tsIdentity(func(i *implementationIdentityInput) {
		i.NonTextContentTypes = []NonTextContentType{NonTextContentImage, NonTextContentImage}
	}) !=
		tsIdentity(func(i *implementationIdentityInput) {
			i.NonTextContentTypes = []NonTextContentType{NonTextContentImage}
		}) {
		t.Error("duplicate image declarations must not change the identity")
	}
	// An unpinned source carries no version.
	unpinned := tsIdentity(func(i *implementationIdentityInput) { i.Implementation = tsUnpinned() })
	if unpinned == base || tsIdentity(func(i *implementationIdentityInput) {
		i.Implementation = tsUnpinned()
		i.Implementation.Source.PackageName = "@example/other-agent"
	}) == unpinned {
		t.Error("unpinned identity cases")
	}
	// Every locked artifact is identified independently.
	locked := tsIdentity(func(i *implementationIdentityInput) { i.Implementation = tsLocked() })
	for name, mut := range map[string]func(*Source){
		"packageJson": func(s *Source) { s.PackageJSON = strings.Replace(tsPackageJSON, "locked-acp-agent", "other-agent", 1) },
		"lock":        func(s *Source) { s.PnpmLockYAML += "\n# changed\n" },
		"workspace":   func(s *Source) { s.PnpmWorkspaceYAML = "allowBuilds:\n  '@example/acp-agent@1.2.3': true\n" },
	} {
		if tsIdentity(func(i *implementationIdentityInput) { i.Implementation = tsLocked(); mut(&i.Implementation.Source) }) == locked {
			t.Errorf("locked %s did not change the identity", name)
		}
	}
	// Provider routing is identified without credentials.
	gw := &providerAuthenticationCompatibility{Type: "ai-gateway", Mode: "ai-gateway", GatewayEnv: map[string]any{"PROVIDER_API_KEY": map[string]any{"$source": "gateway-api-key"}},
		CredentialSource: gatewaySourceAPIKey, BaseURL: "https://gateway.example"}
	gwID := tsIdentity(func(i *implementationIdentityInput) { i.ProviderAuthentication = gw })
	other := *gw
	other.CredentialSource = gatewaySourceOIDCToken
	other2 := *gw
	other2.BaseURL = "https://other-gateway.example"
	for _, c := range []*providerAuthenticationCompatibility{&other, &other2} {
		c := c
		if tsIdentity(func(i *implementationIdentityInput) { i.ProviderAuthentication = c }) == gwID {
			t.Errorf("provider routing change %+v did not change the identity", c)
		}
	}
}

// The expected digests below come from running the TS `createImplementationIdentity`
// and `createACPAuthenticationProfileIdentity` algorithms (implementation.ts,
// acp-auth.ts) in Node on the same inputs, so an ACP lifecycle state can be
// compared across hosts.
func TestTSReplay_IdentityDigestsMatchTS(t *testing.T) {
	mm := ModelMapping{Type: ModelMappingSessionConfigOption, Path: "model"}
	app := ClientApp{Name: "ai-sdk-harness-acp", Version: "0.0.0-test"}
	locked := tsSimple()
	locked.Source = Source{Type: SourceNPMLocked, PackageJSON: "{\"a\":\"<b>&c\"}\n", PnpmLockYAML: "lock: 'x'\n"}
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
		{"simple", createImplementationIdentity(implementationIdentityInput{HarnessID: "example-acp", Implementation: tsSimple(), ClientApp: app, ModelMapping: mm}), "75dc46fd878f72a474f2f6467528e7b7fb66d71a05d996bc47bdeb288fd3bd74"},
		{"locked with html characters", createImplementationIdentity(implementationIdentityInput{HarnessID: "example-acp", Implementation: locked, ClientApp: app, ModelMapping: mm}), "dc13131d76c5c09ee852e5f47d5049c14089fa3e6d9cbae093a928ca4c6b1ef2"},
		{"install command", createImplementationIdentity(implementationIdentityInput{HarnessID: "example-acp", Implementation: install, ClientApp: app, ModelMapping: mm, NonTextContentTypes: []NonTextContentType{NonTextContentImage, NonTextContentImage}}), "767651b769ccfabed25532cb7f89ba3b105254e501363f3a9d4b60ecc3592493"},
		{"gateway", createImplementationIdentity(implementationIdentityInput{HarnessID: "example-acp", Implementation: tsSimple(), ClientApp: app, ClientCapabilities: map[string]any{"_meta": map[string]any{"x": true}},
			ModelMapping: mm, ProviderAuthentication: gw, PermissionModeMapping: pm}), "c9aa9c473d294500a1920ca8d2c2c087328c3648605b7afa4ec666b1ce7cdb6c"},
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
