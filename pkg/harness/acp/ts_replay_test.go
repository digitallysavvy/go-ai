package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

var placeholderRE = regexp.MustCompile(`^aisdkhc_[A-Za-z0-9_-]{43}$`)

// plainSandbox is a fakeSandbox that does not support additive request
// transformations (TS `fakeSandbox` without `addRequestTransformations`).
type plainSandbox struct{ f *fakeSandbox }

func (p plainSandbox) Description() string { return p.f.Description() }
func (p plainSandbox) Run(ctx context.Context, o providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	return p.f.Run(ctx, o)
}
func (p plainSandbox) Spawn(ctx context.Context, o providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	return p.f.Spawn(ctx, o)
}
func (p plainSandbox) ReadFile(ctx context.Context, path string) (io.ReadCloser, error) {
	return p.f.ReadFile(ctx, path)
}
func (p plainSandbox) ReadBinaryFile(ctx context.Context, path string) ([]byte, error) {
	return p.f.ReadBinaryFile(ctx, path)
}
func (p plainSandbox) ReadTextFile(ctx context.Context, o providerutils.SandboxReadTextFileOptions) (*string, error) {
	return p.f.ReadTextFile(ctx, o)
}
func (p plainSandbox) WriteFile(ctx context.Context, path string, r io.Reader) error {
	return p.f.WriteFile(ctx, path, r)
}
func (p plainSandbox) WriteBinaryFile(ctx context.Context, path string, b []byte) error {
	return p.f.WriteBinaryFile(ctx, path, b)
}
func (p plainSandbox) WriteTextFile(ctx context.Context, o providerutils.SandboxWriteTextFileOptions) error {
	return p.f.WriteTextFile(ctx, o)
}
func (p plainSandbox) ID() string                      { return p.f.ID() }
func (p plainSandbox) DefaultWorkingDirectory() string { return p.f.DefaultWorkingDirectory() }
func (p plainSandbox) Ports() []int                    { return p.f.Ports() }
func (p plainSandbox) GetPortEndpoint(ctx context.Context, o harness.PortEndpointOptions) (harness.PortEndpoint, error) {
	return p.f.GetPortEndpoint(ctx, o)
}
func (p plainSandbox) GetPortURL(ctx context.Context, o harness.PortEndpointOptions) (string, error) {
	return p.f.GetPortURL(ctx, o)
}
func (p plainSandbox) Stop(ctx context.Context) error           { return p.f.Stop(ctx) }
func (p plainSandbox) Destroy(ctx context.Context) error        { return p.f.Destroy(ctx) }
func (p plainSandbox) Restricted() providerutils.SandboxSession { return p }

var _ harness.NetworkSandboxSession = plainSandbox{}

func replaySandbox(t *testing.T, token string) *fakeSandbox {
	t.Helper()
	srv := bridgetest.NewServer(bridgetest.Options{Token: token})
	t.Cleanup(srv.Close)
	return newFakeSandbox(srv)
}

func bearerTransformation(host, pathPrefix, sandboxValue, hostValue string) harness.RequestTransformation {
	m := harness.RequestTransformationMatch{
		Host:    host,
		Headers: []harness.KeyValueMatcher{{Key: &harness.StringMatcher{Exact: "Authorization"}, Value: &harness.StringMatcher{Exact: "Bearer " + sandboxValue}}},
	}
	if pathPrefix != "" {
		m.Path = &harness.StringMatcher{StartsWith: pathPrefix}
	}
	return harness.RequestTransformation{Match: m, Transform: harness.RequestTransformationTransform{Headers: map[string]string{"Authorization": "Bearer " + hostValue}}}
}

// TS 'brokers host-resolved direct credentials before launching the sandbox bridge'.
func TestTSReplay_BrokersHostResolvedDirectCredentials(t *testing.T) {
	t.Setenv("HOST_PROVIDER_BASE_URL", "https://host-provider.example/v1")
	sandbox := replaySandbox(t, "tok")
	var gotEnv, gotSandboxEnv map[string]string
	var forwarded []harness.CredentialForwardingOptions
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.ForwardEnv = []string{"PROVIDER_BASE_URL", "HOST_PROVIDER_BASE_URL"}
		s.CredentialEnv = []string{"PROVIDER_API_KEY"}
		s.Env = map[string]string{"STATIC_SETTING": "literal-value"}
		s.ResolveAuthenticationEnvironment = func(context.Context, harness.Authentication, map[string]string) (map[string]string, error) {
			return map[string]string{"PROVIDER_API_KEY": "direct-secret", "PROVIDER_BASE_URL": "https://provider.example/v1"}, nil
		}
		s.CredentialForwarding = func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
			forwarded = append(forwarded, o)
			return "ephemeral-" + o.EnvironmentVariableName, nil
		}
		s.CredentialBrokering = func(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation {
			gotEnv, gotSandboxEnv = env, sandboxEnv
			if sandboxEnv["PROVIDER_API_KEY"] == "" {
				return nil
			}
			return []harness.RequestTransformation{bearerTransformation("provider.example", "/v1", sandboxEnv["PROVIDER_API_KEY"], env["PROVIDER_API_KEY"])}
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/workspace/user-project"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })

	wantEnv := map[string]string{"PROVIDER_API_KEY": "direct-secret", "PROVIDER_BASE_URL": "https://provider.example/v1", "HOST_PROVIDER_BASE_URL": "https://host-provider.example/v1", "STATIC_SETTING": "literal-value"}
	wantSandbox := map[string]string{"PROVIDER_API_KEY": "ephemeral-PROVIDER_API_KEY", "PROVIDER_BASE_URL": "https://provider.example/v1", "HOST_PROVIDER_BASE_URL": "https://host-provider.example/v1", "STATIC_SETTING": "literal-value"}
	if !reflect.DeepEqual(gotEnv, wantEnv) || !reflect.DeepEqual(gotSandboxEnv, wantSandbox) {
		t.Fatalf("brokering env = %v, sandboxEnv = %v", gotEnv, gotSandboxEnv)
	}
	want := bearerTransformation("provider.example", "/v1", "ephemeral-PROVIDER_API_KEY", "direct-secret")
	if len(sandbox.addReqTrans) != 1 || !reflect.DeepEqual(sandbox.addReqTrans[0], []harness.RequestTransformation{want}) {
		t.Fatalf("transformations = %+v", sandbox.addReqTrans)
	}
	if len(forwarded) != 1 || forwarded[0].EnvironmentVariableName != "PROVIDER_API_KEY" || !placeholderRE.MatchString(forwarded[0].Credential) {
		t.Fatalf("forwarded = %+v", forwarded)
	}
	env := sandbox.spawnEnvs[0]
	for k, v := range wantSandbox {
		if env[k] != v {
			t.Errorf("spawn env %s = %q, want %q", k, env[k], v)
		}
	}
	if b, _ := json.Marshal(env); strings.Contains(string(b), "direct-secret") {
		t.Errorf("spawn env leaked: %s", b)
	}
}

// TS 'materializes private authentication files with brokered credentials'.
func TestTSReplay_AuthenticationFilesWithBrokeredCredentials(t *testing.T) {
	t.Setenv("PROVIDER_API_KEY", "host-secret")
	sandbox := replaySandbox(t, "tok")
	var calls int
	var gotEnv, gotSandboxEnv map[string]string
	var gotBrokered bool
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.CredentialEnv = []string{"PROVIDER_API_KEY"}
		s.CredentialBrokering = func(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation { return nil }
		s.CredentialForwarding = func(context.Context, harness.CredentialForwardingOptions) (string, error) {
			return "sandbox-secret", nil
		}
		s.AuthenticationFiles = func(env, sandboxEnv map[string]string, brokered bool) []AuthenticationFile {
			calls++
			gotEnv, gotSandboxEnv, gotBrokered = env, sandboxEnv, brokered
			b, _ := json.Marshal(map[string]string{"token": sandboxEnv["PROVIDER_API_KEY"]})
			return []AuthenticationFile{{Path: ".config/provider/auth.json", Content: string(b)}}
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/workspace/user-project"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if calls != 1 || gotEnv["PROVIDER_API_KEY"] != "host-secret" || gotSandboxEnv["PROVIDER_API_KEY"] != "sandbox-secret" || !gotBrokered {
		t.Fatalf("authenticationFiles called %d times with env=%v sandboxEnv=%v brokered=%v", calls, gotEnv, gotSandboxEnv, gotBrokered)
	}
	if got := sandbox.files["/home/vercel-sandbox/.config/provider/auth.json"]; got != `{"token":"sandbox-secret"}` {
		t.Fatalf("files = %v", sandbox.files)
	}
	foundChmod := false
	for _, r := range sandbox.runs {
		if r == "chmod 600 -- '/home/vercel-sandbox/.config/provider/auth.json'" {
			foundChmod = true
		}
	}
	if !foundChmod {
		t.Fatalf("runs = %q", sandbox.runs)
	}
	for _, c := range sandbox.files {
		if strings.Contains(c, "host-secret") {
			t.Fatalf("a file contains the host secret: %s", c)
		}
	}
}

// TS 'uses resolved Gateway profile values for brokering without serializing the credential'.
func TestTSReplay_GatewayProfileBrokering(t *testing.T) {
	t.Setenv("PROVIDER_API_KEY", "direct-secret")
	t.Setenv("AI_GATEWAY_API_KEY", "gateway-secret")
	t.Setenv("AI_GATEWAY_BASE_URL", "https://gateway.example")
	sandbox := replaySandbox(t, "tok")
	var gotEnv, gotSandboxEnv, gotHeaders map[string]string
	var forwarded []harness.CredentialForwardingOptions
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.HarnessID = "gateway-brokered-acp"
		s.Auth = harness.Authentication{Mode: harness.AuthModeAIGateway}
		s.ForwardEnv = nil
		s.CredentialEnv = []string{"PROVIDER_API_KEY"}
		s.ProviderAuthentication = &ProviderAuthentication{GatewayEnv: map[string]any{
			"PROVIDER_API_KEY":  map[string]any{"$source": "gateway-api-key"},
			"PROVIDER_BASE_URL": map[string]any{"$source": "gateway-base-url", "ensureSuffix": "/v1"},
		}}
		s.CredentialForwarding = func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
			forwarded = append(forwarded, o)
			return "ephemeral-" + o.EnvironmentVariableName, nil
		}
		s.CredentialBrokering = func(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation {
			gotEnv, gotSandboxEnv, gotHeaders = env, sandboxEnv, headers
			if sandboxEnv["PROVIDER_API_KEY"] == "" {
				return nil
			}
			return []harness.RequestTransformation{bearerTransformation("gateway.example", "/v1", sandboxEnv["PROVIDER_API_KEY"], env["PROVIDER_API_KEY"])}
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/workspace/user-project", Headers: map[string]string{"x-tenant": "acme"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if !reflect.DeepEqual(gotEnv, map[string]string{"PROVIDER_API_KEY": "gateway-secret", "PROVIDER_BASE_URL": "https://gateway.example/v1"}) ||
		!reflect.DeepEqual(gotSandboxEnv, map[string]string{"PROVIDER_API_KEY": "ephemeral-PROVIDER_API_KEY", "PROVIDER_BASE_URL": "https://gateway.example/v1"}) ||
		!reflect.DeepEqual(gotHeaders, map[string]string{"x-tenant": "acme"}) {
		t.Fatalf("env=%v sandboxEnv=%v headers=%v", gotEnv, gotSandboxEnv, gotHeaders)
	}
	want := bearerTransformation("gateway.example", "/v1", "ephemeral-PROVIDER_API_KEY", "gateway-secret")
	if len(sandbox.addReqTrans) != 1 || !reflect.DeepEqual(sandbox.addReqTrans[0], []harness.RequestTransformation{want}) {
		t.Fatalf("transformations = %+v", sandbox.addReqTrans)
	}
	env := sandbox.spawnEnvs[0]
	if env["PROVIDER_API_KEY"] != "ephemeral-PROVIDER_API_KEY" || env["PROVIDER_BASE_URL"] != "https://gateway.example/v1" {
		t.Fatalf("spawn env = %v", env)
	}
	if _, ok := env["AI_SDK_ACP_GATEWAY_API_KEY"]; ok {
		t.Error("gateway api key env var must be withheld")
	}
	if b, _ := json.Marshal(env); strings.Contains(string(b), "gateway-secret") {
		t.Errorf("spawn env leaked: %s", b)
	}
	cfg := bridgeConfig(t, env)
	if pe, ok := cfg["providerEnvironment"].(map[string]any); !ok || len(pe) != 0 || cfg["providerAuthentication"].(map[string]any)["type"] != "ai-gateway" {
		t.Errorf("bridge configuration = %v", cfg)
	}
	if len(forwarded) != 1 || forwarded[0].EnvironmentVariableName != "PROVIDER_API_KEY" || !placeholderRE.MatchString(forwarded[0].Credential) {
		t.Errorf("forwarded = %+v", forwarded)
	}
}

// TS 'uses the exposed environment variable name for brokered Gateway credentials'.
func TestTSReplay_GatewayCredentialUsesExposedVariableName(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "gateway-secret")
	sandbox := replaySandbox(t, "tok")
	var gotEnv, gotSandboxEnv map[string]string
	var forwarded []harness.CredentialForwardingOptions
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.HarnessID = "renamed-gateway-credential-acp"
		s.Auth = harness.Authentication{Mode: harness.AuthModeAIGateway}
		s.ForwardEnv = nil
		s.CredentialEnv = []string{}
		s.ProviderAuthentication = &ProviderAuthentication{GatewayEnv: map[string]any{"RUNTIME_PROVIDER_API_KEY": map[string]any{"$source": "gateway-api-key"}}}
		s.CredentialForwarding = func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
			forwarded = append(forwarded, o)
			return o.Credential, nil
		}
		s.CredentialBrokering = func(env, sandboxEnv, headers map[string]string) []harness.RequestTransformation {
			gotEnv, gotSandboxEnv = env, sandboxEnv
			if sandboxEnv["RUNTIME_PROVIDER_API_KEY"] == "" {
				return nil
			}
			return []harness.RequestTransformation{bearerTransformation("gateway.example", "", sandboxEnv["RUNTIME_PROVIDER_API_KEY"], env["RUNTIME_PROVIDER_API_KEY"])}
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/workspace/user-project"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	placeholder := gotSandboxEnv["RUNTIME_PROVIDER_API_KEY"]
	if !placeholderRE.MatchString(placeholder) || !reflect.DeepEqual(gotEnv, map[string]string{"RUNTIME_PROVIDER_API_KEY": "gateway-secret"}) {
		t.Fatalf("env=%v sandboxEnv=%v", gotEnv, gotSandboxEnv)
	}
	if len(forwarded) != 1 || forwarded[0].Credential != placeholder || forwarded[0].EnvironmentVariableName != "RUNTIME_PROVIDER_API_KEY" {
		t.Fatalf("forwarded = %+v", forwarded)
	}
	want := bearerTransformation("gateway.example", "", placeholder, "gateway-secret")
	if len(sandbox.addReqTrans) != 1 || !reflect.DeepEqual(sandbox.addReqTrans[0], []harness.RequestTransformation{want}) {
		t.Fatalf("transformations = %+v", sandbox.addReqTrans)
	}
	env := sandbox.spawnEnvs[0]
	if env["RUNTIME_PROVIDER_API_KEY"] != placeholder {
		t.Errorf("spawn env = %v", env)
	}
	if b, _ := json.Marshal(env); strings.Contains(string(b), "gateway-secret") {
		t.Errorf("spawn env leaked: %s", b)
	}
}

// TS 'persists sandbox credentials and reinstalls the same matchers across process resumes'.
func TestTSReplay_PersistsSandboxCredentialsAcrossResumes(t *testing.T) {
	t.Setenv("PROVIDER_API_KEY", "direct-secret")
	sandbox := replaySandbox(t, "tok")
	create := func() harness.Harness {
		h, err := CreateACP(testSettings(func(s *Settings) {
			s.MintBridgeToken = func(string) string { return "tok" }
			s.HarnessID = "resumable-brokered-acp"
			s.ForwardEnv = nil
			s.CredentialEnv = []string{"PROVIDER_API_KEY"}
			s.CredentialBrokering = func(env, sandboxEnv, _ map[string]string) []harness.RequestTransformation {
				if sandboxEnv["PROVIDER_API_KEY"] == "" {
					return nil
				}
				return []harness.RequestTransformation{bearerTransformation("provider.example", "", sandboxEnv["PROVIDER_API_KEY"], env["PROVIDER_API_KEY"])}
			}
		}))
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	first, err := create().DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/workspace/user-project"})
	if err != nil {
		t.Fatal(err)
	}
	resume, err := first.DoDetach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		SandboxCredentialEnvironment map[string]string `json:"sandboxCredentialEnvironment"`
	}
	_ = json.Unmarshal(resume.Data, &data)
	persisted := data.SandboxCredentialEnvironment["PROVIDER_API_KEY"]
	if !placeholderRE.MatchString(persisted) || sandbox.spawnEnvs[0]["PROVIDER_API_KEY"] != persisted {
		t.Fatalf("persisted = %q, spawn env = %v", persisted, sandbox.spawnEnvs[0])
	}
	if strings.Contains(string(resume.Data), "direct-secret") {
		t.Fatalf("resume state leaked the secret: %s", resume.Data)
	}
	var roundTrip harness.ResumeSessionState
	b, _ := json.Marshal(resume)
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatal(err)
	}
	resumed, err := create().DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/workspace/user-project", ResumeFrom: &roundTrip})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resumed.DoDestroy(context.Background()) })
	if len(sandbox.spawnCommands) != 1 {
		t.Fatalf("spawns = %d, want 1 (attach)", len(sandbox.spawnCommands))
	}
	if len(sandbox.addReqTrans) != 2 || !reflect.DeepEqual(sandbox.addReqTrans[0], sandbox.addReqTrans[1]) {
		t.Fatalf("transformations = %+v", sandbox.addReqTrans)
	}
	want := bearerTransformation("provider.example", "", persisted, "direct-secret")
	if !reflect.DeepEqual(sandbox.addReqTrans[1], []harness.RequestTransformation{want}) {
		t.Fatalf("transformations[1] = %+v", sandbox.addReqTrans[1])
	}
}

// TS 'preserves real credential forwarding when additive transformations are unavailable'.
func TestTSReplay_RealForwardingWithoutAdditiveTransformations(t *testing.T) {
	t.Setenv("PROVIDER_API_KEY", "legacy-secret")
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
	t.Cleanup(srv.Close)
	sandbox := newFakeSandbox(srv)
	called := false
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.ForwardEnv = nil
		s.CredentialEnv = []string{"PROVIDER_API_KEY"}
		s.CredentialBrokering = func(env, sandboxEnv, _ map[string]string) []harness.RequestTransformation {
			called = true
			return nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: plainSandbox{sandbox}, SessionWorkDir: "/workspace/user-project"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if called {
		t.Fatal("credentialBrokering must not run without additive transformations")
	}
	if sandbox.spawnEnvs[0]["PROVIDER_API_KEY"] != "legacy-secret" {
		t.Fatalf("spawn env = %v", sandbox.spawnEnvs[0])
	}
}

// TS 'customizes direct and Gateway credentials under their sandbox environment names'.
func TestTSReplay_CustomizesDirectAndGatewayCredentials(t *testing.T) {
	t.Setenv("PROVIDER_API_KEY", "direct-secret")
	t.Setenv("AI_GATEWAY_API_KEY", "gateway-secret")
	sandbox := replaySandbox(t, "tok")
	var forwarded []harness.CredentialForwardingOptions
	called := false
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.HarnessID = "custom-forwarding-acp"
		s.Auth = harness.Authentication{Mode: harness.AuthModeAIGateway}
		s.ForwardEnv = nil
		s.CredentialEnv = []string{"PROVIDER_API_KEY"}
		s.CredentialBrokering = func(env, sandboxEnv, _ map[string]string) []harness.RequestTransformation { called = true; return nil }
		s.ProviderAuthentication = &ProviderAuthentication{GatewayEnv: map[string]any{"RUNTIME_PROVIDER_API_KEY": map[string]any{"$source": "gateway-api-key"}}}
		s.CredentialForwarding = func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
			forwarded = append(forwarded, o)
			if o.Credential == "gateway-secret" {
				return "ephemeral-gateway-credential", nil
			}
			return "ephemeral-direct-credential", nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: plainSandbox{sandbox}, SessionWorkDir: "/workspace/user-project"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if called {
		t.Fatal("brokering must not run")
	}
	want := []harness.CredentialForwardingOptions{
		{Credential: "direct-secret", EnvironmentVariableName: "PROVIDER_API_KEY"},
		{Credential: "gateway-secret", EnvironmentVariableName: "RUNTIME_PROVIDER_API_KEY"},
	}
	if !reflect.DeepEqual(forwarded, want) {
		t.Fatalf("forwarded = %+v, want %+v", forwarded, want)
	}
	env := sandbox.spawnEnvs[0]
	if env["PROVIDER_API_KEY"] != "ephemeral-direct-credential" || env["RUNTIME_PROVIDER_API_KEY"] != "ephemeral-gateway-credential" {
		t.Fatalf("spawn env = %v", env)
	}
	if _, ok := env["AI_SDK_ACP_GATEWAY_API_KEY"]; ok {
		t.Error("gateway api key env var must be withheld")
	}
	if pe, ok := bridgeConfig(t, env)["providerEnvironment"].(map[string]any); !ok || len(pe) != 0 {
		t.Errorf("providerEnvironment = %v", bridgeConfig(t, env)["providerEnvironment"])
	}
}

// TS 'aborts before spawning when credential forwarding fails' / 'aborts before
// attach or spawn when credential brokering fails'.
func TestTSReplay_CredentialFailuresAbortBeforeSpawn(t *testing.T) {
	t.Setenv("PROVIDER_API_KEY", "direct-secret")
	t.Run("forwarding", func(t *testing.T) {
		sandbox := replaySandbox(t, "tok")
		boom := errors.New("credential forwarding failed")
		h, _ := CreateACP(testSettings(func(s *Settings) {
			s.ForwardEnv = nil
			s.CredentialEnv = []string{"PROVIDER_API_KEY"}
			s.CredentialBrokering = func(_, _, _ map[string]string) []harness.RequestTransformation { return nil }
			s.CredentialForwarding = func(context.Context, harness.CredentialForwardingOptions) (string, error) { return "", boom }
		}))
		_, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: plainSandbox{sandbox}, SessionWorkDir: "/w"})
		if !errors.Is(err, boom) || len(sandbox.spawnCommands) != 0 {
			t.Fatalf("err = %v, spawns = %d", err, len(sandbox.spawnCommands))
		}
	})
	t.Run("brokering", func(t *testing.T) {
		srv := bridgetest.NewServer(bridgetest.Options{Token: "tok"})
		t.Cleanup(srv.Close)
		sandbox := &failingAdder{fakeSandbox: newFakeSandbox(srv), err: errors.New("request transformation installation failed")}
		h, _ := CreateACP(testSettings(func(s *Settings) {
			s.ForwardEnv = nil
			s.CredentialEnv = []string{"PROVIDER_API_KEY"}
			s.CredentialBrokering = func(env, sandboxEnv, _ map[string]string) []harness.RequestTransformation {
				return []harness.RequestTransformation{bearerTransformation("provider.example", "", sandboxEnv["PROVIDER_API_KEY"], env["PROVIDER_API_KEY"])}
			}
		}))
		_, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/w"})
		if !errors.Is(err, sandbox.err) || len(sandbox.spawnCommands) != 0 {
			t.Fatalf("err = %v, spawns = %d", err, len(sandbox.spawnCommands))
		}
	})
}

type failingAdder struct {
	*fakeSandbox
	err error
}

func (f *failingAdder) Restricted() providerutils.SandboxSession { return f }
func (f *failingAdder) AddRequestTransformations(context.Context, []harness.RequestTransformation) error {
	return f.err
}

// TS 'validates lifecycle state structurally and rejects incompatible identities at start'.
func TestTSReplay_LifecycleIdentityMismatchRejected(t *testing.T) {
	sandbox := replaySandbox(t, "tok")
	mk := func(args ...string) harness.Harness {
		h, err := CreateACP(testSettings(func(s *Settings) {
			s.MintBridgeToken = func(string) string { return "tok" }
			s.HarnessID = "identity-acp"
			s.Args = args
		}))
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	first, second := mk(), mk("different")
	sess, err := first.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	resume, err := sess.DoStop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := second.(harness.LifecycleStateValidator); !ok || v.ValidateLifecycleStateData(resume.Data) != nil {
		t.Fatal("a structurally valid state must validate against any identity")
	}
	// A legacy providerKind spelling stays structurally valid.
	var data map[string]any
	_ = json.Unmarshal(resume.Data, &data)
	data["authenticationProfile"].(map[string]any)["providerKind"] = "implementation-default"
	legacy, _ := json.Marshal(data)
	if err := first.(harness.LifecycleStateValidator).ValidateLifecycleStateData(legacy); err != nil {
		t.Fatalf("legacy providerKind rejected: %v", err)
	}
	_, err = second.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/w", ResumeFrom: resume})
	if err == nil || !strings.Contains(err.Error(), "ACP lifecycle state is incompatible with the configured implementation.") {
		t.Fatalf("err = %v", err)
	}
	// Structurally broken state data is rejected up front.
	if err := first.(harness.LifecycleStateValidator).ValidateLifecycleStateData(json.RawMessage(`{"bridge":{"port":1}}`)); err == nil {
		t.Fatal("state without implementationIdentity must be rejected")
	}
}

// finishingServer answers every `start` with a finish and records the start frames.
func finishingServer(t *testing.T) (*bridgetest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var starts []map[string]any
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, start map[string]any) {
		mu.Lock()
		starts = append(starts, start)
		mu.Unlock()
		turn.Emit(finishFrame())
	}})
	t.Cleanup(srv.Close)
	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), starts...)
	}
}

func runTurn(t *testing.T, sess harness.Session, opts harness.PromptTurnOptions) {
	t.Helper()
	opts.Emit = func(harness.StreamPart) {}
	c, err := sess.DoPromptTurn(context.Background(), opts)
	if err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	<-c.Done()
	if c.Err() != nil {
		t.Fatalf("turn error: %v", c.Err())
	}
}

func promptTexts(start map[string]any) []string {
	var out []string
	for _, b := range start["prompt"].([]any) {
		out = append(out, b.(map[string]any)["text"].(string))
	}
	return out
}

// TS 'materializes skills in the native directory without adding them to prompt guidance'.
func TestTSReplay_SkillsMaterializedNatively(t *testing.T) {
	srv, starts := finishingServer(t)
	sandbox := newFakeSandbox(srv)
	h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" } }))
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/workspace/user-project"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	skills := []harness.Skill{{Name: "release-notes", Description: "Prepare concise release notes.", Content: "Full private skill instructions.",
		Files: []harness.SkillFile{{Path: "references/style.md", Content: "Use active voice."}}}}
	runTurn(t, sess, harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Skills: skills, Instructions: "Use the supplied project context."},
		Prompt:       harness.Prompt{Message: &types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Draft"}, types.TextContent{Text: "the notes."}}}},
	})
	if got := sandbox.files["/home/vercel-sandbox/.agents/skills/release-notes/SKILL.md"]; !strings.Contains(got, "Full private skill instructions.") {
		t.Fatalf("skill file missing: %v", sandbox.files)
	}
	if sandbox.files["/home/vercel-sandbox/.agents/skills/release-notes/references/style.md"] != "Use active voice." {
		t.Fatalf("reference file missing")
	}
	first := starts()[0]
	texts := promptTexts(first)
	if len(texts) != 3 || !strings.Contains(texts[0], "<session-guidance>") || !strings.Contains(texts[0], "Use the supplied project context.") || texts[1] != "Draft" || texts[2] != "the notes." {
		t.Fatalf("first prompt = %q", texts)
	}
	if b, _ := json.Marshal(first["prompt"]); strings.Contains(string(b), "release-notes") || strings.Contains(string(b), "Full private skill instructions.") || strings.Contains(string(b), "Use active voice.") {
		t.Fatalf("skills leaked into the prompt: %s", b)
	}
	runTurn(t, sess, harness.PromptTurnOptions{
		TurnSettings: harness.TurnSettings{Skills: skills, Instructions: "Use the revised project context."}, Prompt: harness.TextPrompt("Revise them."),
	})
	second := promptTexts(starts()[1])
	if len(second) != 2 || !strings.Contains(second[0], "Use the revised project context.") || second[1] != "Revise them." {
		t.Fatalf("second prompt = %q", second)
	}
	state, err := sess.DoStop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	_ = json.Unmarshal(state.Data, &data)
	if data["initialGuidanceApplied"] != true || data["skillsDirectory"] != "/home/vercel-sandbox/.agents/skills" {
		t.Fatalf("lifecycle data = %v", data)
	}
	if _, ok := data["skillsFingerprint"]; ok {
		t.Error("skillsFingerprint must not be persisted")
	}
}

// TS 'uses native instructions initially and prompt guidance when they change'
// and 'materializes instructions into the filesystem and never prepends prompt guidance'.
func TestTSReplay_InstructionMappings(t *testing.T) {
	t.Run("session-meta", func(t *testing.T) {
		srv, starts := finishingServer(t)
		sandbox := newFakeSandbox(srv)
		h, _ := CreateACP(testSettings(func(s *Settings) {
			s.MintBridgeToken = func(string) string { return "tok" }
			s.SkillsDirectory = ".claude/skills"
			s.InstructionMapping = &InstructionMapping{Type: InstructionMappingSessionMeta, Path: []string{"systemPrompt", "append"}}
		}))
		sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/workspace/user-project"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
		skills := []harness.Skill{{Name: "release-notes", Description: "Prepare concise release notes.", Content: "Use active voice."}}
		runTurn(t, sess, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Skills: skills, Instructions: "Answer every question in German."}, Prompt: harness.TextPrompt("Draft release notes.")})
		first := starts()[0]
		if first["instructions"] != "Answer every question in German." || !reflect.DeepEqual(promptTexts(first), []string{"Draft release notes."}) {
			t.Fatalf("first start = %v", first)
		}
		if got := sandbox.files["/home/vercel-sandbox/.claude/skills/release-notes/SKILL.md"]; got != "---\nname: release-notes\ndescription: Prepare concise release notes.\n---\n\nUse active voice." {
			t.Fatalf("SKILL.md = %q", got)
		}
		runTurn(t, sess, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Skills: skills, Instructions: "Answer every question in French."}, Prompt: harness.TextPrompt("Revise them.")})
		second := starts()[1]
		texts := promptTexts(second)
		if second["instructions"] != "Answer every question in French." || len(texts) != 2 || !strings.Contains(texts[0], "Answer every question in French.") || texts[1] != "Revise them." {
			t.Fatalf("second start = %v", second)
		}
	})
	t.Run("filesystem", func(t *testing.T) {
		srv, starts := finishingServer(t)
		sandbox := newFakeSandbox(srv)
		h, _ := CreateACP(testSettings(func(s *Settings) {
			s.MintBridgeToken = func(string) string { return "tok" }
			s.InstructionMapping = &InstructionMapping{Type: InstructionMappingFilesystem, FilePath: ".cursor/rules/AGENTS.md"}
		}))
		sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/workspace/user-project"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
		runTurn(t, sess, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Instructions: "Always run tests."}, Prompt: harness.TextPrompt("Draft release notes.")})
		if got := sandbox.files["/home/vercel-sandbox/.cursor/rules/AGENTS.md"]; got != "Always run tests.\n" {
			t.Fatalf("AGENTS.md = %q", got)
		}
		runTurn(t, sess, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Instructions: "Answer every question in French."}, Prompt: harness.TextPrompt("Revise them.")})
		if got := sandbox.files["/home/vercel-sandbox/.cursor/rules/AGENTS.md"]; got != "Answer every question in French.\n" {
			t.Fatalf("AGENTS.md = %q", got)
		}
		for i, want := range []string{"Draft release notes.", "Revise them."} {
			if !reflect.DeepEqual(promptTexts(starts()[i]), []string{want}) {
				t.Fatalf("start %d prompt = %q", i, promptTexts(starts()[i]))
			}
		}
	})
}

// TS 'replaces changed skills before starting the next turn'.
func TestTSReplay_ChangedSkillsReplaced(t *testing.T) {
	srv, _ := finishingServer(t)
	sandbox := newFakeSandbox(srv)
	h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" } }))
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/workspace/user-project"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	runTurn(t, sess, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Skills: []harness.Skill{{Name: "review-workflow", Description: "Review changes.", Content: "Review the implementation."}}}, Prompt: harness.TextPrompt("Review this change.")})
	runTurn(t, sess, harness.PromptTurnOptions{TurnSettings: harness.TurnSettings{Skills: []harness.Skill{{Name: "release-workflow", Description: "Prepare releases.", Content: "Draft the release notes."}}}, Prompt: harness.TextPrompt("Prepare the release.")})
	found := false
	for _, r := range sandbox.runs {
		if r == "rm -rf -- '/home/vercel-sandbox/.agents/skills/review-workflow'" {
			found = true
		}
	}
	if !found {
		t.Fatalf("runs = %q", sandbox.runs)
	}
	if _, ok := sandbox.files["/home/vercel-sandbox/.agents/skills/release-workflow/SKILL.md"]; !ok {
		t.Fatalf("new skill not written: %v", sandbox.files)
	}
}

// TS 'rejects structured output when the ACP profile has no schema mapping' and
// 'passes structured output configuration to a mapped ACP profile'.
func TestTSReplay_StructuredOutputMapping(t *testing.T) {
	srv, starts := finishingServer(t)
	sandbox := newFakeSandbox(srv)
	rf := &harness.ResponseFormat{Type: harness.ResponseFormatJSON, Schema: map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}}, "required": []any{"answer"}}}
	h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" } }))
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	_, err = sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Answer."), ResponseFormat: rf, Emit: func(harness.StreamPart) {}})
	var unsupported *harness.CapabilityUnsupportedError
	if !errors.As(err, &unsupported) || len(starts()) != 0 {
		t.Fatalf("err = %v, starts = %d", err, len(starts()))
	}

	srv2, starts2 := finishingServer(t)
	h2, _ := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.OutputSchemaMapping = &OutputSchemaMapping{Path: []string{"outputSchema"}}
	}))
	sess2, err := h2.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: newFakeSandbox(srv2), SessionWorkDir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess2.DoDestroy(context.Background()) })
	runTurn(t, sess2, harness.PromptTurnOptions{Prompt: harness.TextPrompt("Answer."), ResponseFormat: rf})
	start := starts2()[0]
	if !reflect.DeepEqual(start["outputSchemaMapping"], map[string]any{"type": "session-prompt-meta", "path": []any{"outputSchema"}}) {
		t.Fatalf("outputSchemaMapping = %v", start["outputSchemaMapping"])
	}
	if rfm, _ := start["responseFormat"].(map[string]any); rfm["type"] != "json" {
		t.Fatalf("responseFormat = %v", start["responseFormat"])
	}
}

// TS 'detaches between turns and attaches without spawning or reapplying
// guidance and skills' and the cold-restore equivalent: the guidance
// bookkeeping (initialGuidanceApplied, instructions fingerprint) survives
// every resume path, so unchanged instructions are not prepended again.
func TestTSReplay_ResumeDoesNotReapplyGuidance(t *testing.T) {
	const instructions = "Remember user-provided facts."
	for _, path := range []string{"attach", "cold-restore"} {
		t.Run(path, func(t *testing.T) {
			var mu sync.Mutex
			var starts []map[string]any
			srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, start map[string]any) {
				mu.Lock()
				starts = append(starts, start)
				n := len(starts)
				mu.Unlock()
				if n == 1 {
					turn.Emit(map[string]any{"type": "bridge-thread", "threadId": "acp-session-1"})
				} else if tsc, _ := start["turnStartConfig"].(map[string]any); start["recoveryMode"] != nil && tsc != nil {
					turn.Emit(map[string]any{"type": "raw", "rawValue": map[string]any{"type": "acp-session-restored", "method": "resume"}})
				}
				turn.Emit(finishFrame())
			}})
			t.Cleanup(srv.Close)
			sandbox := newFakeSandbox(srv)
			h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" } }))
			ctx := context.Background()
			first, err := h.DoStart(ctx, harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/w"})
			if err != nil {
				t.Fatal(err)
			}
			skills := []harness.Skill{{Name: "private-context", Description: "Use private context.", Content: "Private skill body."}}
			runTurn(t, first, harness.PromptTurnOptions{Prompt: harness.TextPrompt("Remember the number 42."), TurnSettings: harness.TurnSettings{Skills: skills, Instructions: instructions}})
			var resume *harness.ResumeSessionState
			if path == "attach" {
				resume, err = first.DoDetach(ctx)
			} else {
				resume, err = first.DoStop(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			var data map[string]any
			_ = json.Unmarshal(resume.Data, &data)
			if data["initialGuidanceApplied"] != true {
				t.Fatalf("lifecycle data = %v", data)
			}
			writes := len(sandbox.files)
			second, err := h.DoStart(ctx, harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/w", ResumeFrom: resume})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = second.DoDestroy(ctx) })
			if path == "attach" && len(sandbox.spawnCommands) != 1 {
				t.Fatalf("spawns = %d, want 1 (attach)", len(sandbox.spawnCommands))
			}
			if len(sandbox.files) != writes {
				t.Fatalf("files written during resume: %d -> %d", writes, len(sandbox.files))
			}
			runTurn(t, second, harness.PromptTurnOptions{Prompt: harness.TextPrompt("What number did I ask you to remember?"), TurnSettings: harness.TurnSettings{Skills: skills, Instructions: instructions}})
			mu.Lock()
			last := starts[len(starts)-1]
			mu.Unlock()
			if !reflect.DeepEqual(promptTexts(last), []string{"What number did I ask you to remember?"}) {
				t.Fatalf("prompt after %s = %q", path, promptTexts(last))
			}
			if b, _ := json.Marshal(last); strings.Contains(string(b), "<session-guidance>") || strings.Contains(string(b), "Private skill body.") {
				t.Fatalf("guidance or skill body re-sent: %s", b)
			}
		})
	}
}

// TS 'preserves capability errors reported by the sandbox bridge'.
func TestTSReplay_PreservesBridgeCapabilityErrors(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, _ map[string]any) {
		turn.Emit(map[string]any{"type": "error", "error": map[string]any{"name": "AI_HarnessBridgeCapabilityUnsupportedError", "message": "The catalog was not refreshed."}})
	}})
	t.Cleanup(srv.Close)
	h, _ := CreateACP(testSettings(func(s *Settings) { s.MintBridgeToken = func(string) string { return "tok" }; s.HarnessID = "codex-acp" }))
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: newFakeSandbox(srv), SessionWorkDir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	var parts []harness.StreamPart
	c, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Use time."), Emit: func(p harness.StreamPart) { parts = append(parts, p) }})
	if err != nil {
		t.Fatal(err)
	}
	<-c.Done()
	var unsupported *harness.CapabilityUnsupportedError
	if !errors.As(c.Err(), &unsupported) || unsupported.HarnessID != "codex-acp" || !strings.Contains(unsupported.Error(), "The catalog was not refreshed.") {
		t.Fatalf("control.Err() = %#v", c.Err())
	}
	if len(parts) != 1 {
		t.Fatalf("parts = %d", len(parts))
	}
	ep, ok := parts[0].(*harness.ErrorPart)
	if !ok {
		t.Fatalf("part = %T", parts[0])
	}
	if e, _ := ep.Error.(error); !errors.As(e, &unsupported) {
		t.Fatalf("forwarded error = %#v, want a *CapabilityUnsupportedError", ep.Error)
	}
}

// TS 'resolves downstream Gateway credentials separately from bridge auth',
// 'excludes resolved credentials from ... lifecycle state' and
// 'excludes direct provider credentials from its authentication profile'.
func TestTSReplay_ProviderAuthentication(t *testing.T) {
	gatewayProfile := &ProviderAuthentication{GatewayEnv: map[string]any{"PROVIDER_API_KEY": map[string]any{"$source": "gateway-api-key"}}}
	cases := []struct {
		name  string
		env   map[string]string
		mut   func(*Settings)
		check func(t *testing.T, spawn map[string]string, profile map[string]any, state string)
	}{
		{"gateway credentials separate from bridge auth",
			map[string]string{"AI_GATEWAY_API_KEY": "gateway-key", "AI_GATEWAY_BASE_URL": "https://gateway.example/custom"},
			func(s *Settings) {
				s.Auth = harness.Authentication{Mode: harness.AuthModeAIGateway}
				s.ClientApp = ClientApp{Name: "custom-client", Version: "1.2.3"}
				s.ForwardEnv = nil
				s.ProviderAuthentication = &ProviderAuthentication{GatewayEnv: map[string]any{}}
			},
			func(t *testing.T, env map[string]string, _ map[string]any, _ string) {
				if env["AI_SDK_ACP_GATEWAY_API_KEY"] != "gateway-key" || env["AI_SDK_ACP_GATEWAY_BASE_URL"] != "https://gateway.example/custom" ||
					env["AI_SDK_ACP_CLIENT_APP_NAME"] != "custom-client" || env["AI_SDK_ACP_CLIENT_APP_VERSION"] != "1.2.3" || env["BRIDGE_CHANNEL_TOKEN"] == "gateway-key" || env["CODEX_API_KEY"] != "" {
					t.Fatalf("env = %v", env)
				}
			}},
		{"credentials excluded from lifecycle state (gateway)",
			map[string]string{"PROVIDER_API_KEY": "direct-secret", "AI_GATEWAY_API_KEY": "gateway-secret", "AI_GATEWAY_BASE_URL": "https://gateway.example"},
			func(s *Settings) {
				s.Auth = harness.Authentication{Mode: harness.AuthModeAIGateway}
				s.ForwardEnv = []string{"PROVIDER_API_KEY"}
				s.Env = map[string]string{"PROVIDER_BASE_URL": "https://provider.example"}
				s.ProviderAuthentication = gatewayProfile
			},
			func(t *testing.T, env map[string]string, profile map[string]any, state string) {
				if env["PROVIDER_API_KEY"] != "direct-secret" || env["PROVIDER_BASE_URL"] != "https://provider.example" || env["AI_SDK_ACP_GATEWAY_API_KEY"] != "gateway-secret" {
					t.Fatalf("env = %v", env)
				}
				if strings.Contains(state, "direct-secret") || strings.Contains(state, "gateway-secret") {
					t.Fatalf("state leaked a secret: %s", state)
				}
				if profile["providerKind"] != "ai-gateway" || profile["providerMode"] != "ai-gateway" || profile["gatewayCredentialSource"] != "AI_GATEWAY_API_KEY" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(profile["digest"].(string)) {
					t.Fatalf("profile = %v", profile)
				}
			}},
		{"direct credentials excluded from the profile",
			map[string]string{"PROVIDER_API_KEY": "direct-profile-secret", "AI_GATEWAY_API_KEY": "unused-gateway-secret"},
			func(s *Settings) {
				s.Auth = harness.Authentication{Mode: harness.AuthModeDirect}
				s.ForwardEnv = []string{"PROVIDER_API_KEY"}
				s.ProviderAuthentication = gatewayProfile
			},
			func(t *testing.T, env map[string]string, profile map[string]any, state string) {
				if env["PROVIDER_API_KEY"] != "direct-profile-secret" || env["AI_SDK_ACP_GATEWAY_API_KEY"] != "" {
					t.Fatalf("env = %v", env)
				}
				if strings.Contains(state, "direct-profile-secret") || strings.Contains(state, "unused-gateway-secret") {
					t.Fatalf("state leaked a secret: %s", state)
				}
				if profile["providerKind"] != "direct" || profile["providerMode"] != "direct" {
					t.Fatalf("profile = %v", profile)
				}
			}},
		{"automatic mode resolves from the session-start environment",
			map[string]string{"AI_GATEWAY_API_KEY": "late-gateway-key", "AI_GATEWAY_BASE_URL": "https://gateway.example/late"},
			func(s *Settings) {
				s.ForwardEnv = nil
				s.ProviderAuthentication = gatewayProfile
			},
			func(t *testing.T, env map[string]string, profile map[string]any, _ string) {
				if env["AI_SDK_ACP_GATEWAY_API_KEY"] != "late-gateway-key" || env["AI_SDK_ACP_GATEWAY_BASE_URL"] != "https://gateway.example/late" {
					t.Fatalf("env = %v", env)
				}
				if profile["providerKind"] != "ai-gateway" || profile["providerMode"] != "auto" || profile["gatewayCredentialSource"] != "AI_GATEWAY_API_KEY" {
					t.Fatalf("profile = %v", profile)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"AI_GATEWAY_API_KEY", "VERCEL_OIDC_TOKEN", "AI_GATEWAY_BASE_URL", "PROVIDER_API_KEY"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			sandbox := replaySandbox(t, "tok")
			h, err := CreateACP(testSettings(func(s *Settings) {
				s.MintBridgeToken = func(string) string { return "tok" }
				s.HarnessID = "codex-acp"
				tc.mut(s)
			}))
			if err != nil {
				t.Fatal(err)
			}
			sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: sandbox, SessionWorkDir: "/w"})
			if err != nil {
				t.Fatal(err)
			}
			state, err := sess.DoStop(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var data map[string]any
			_ = json.Unmarshal(state.Data, &data)
			profile, _ := data["authenticationProfile"].(map[string]any)
			tc.check(t, sandbox.spawnEnvs[0], profile, string(state.Data))
		})
	}
}

// TS 'translates ACP question requests and client results'.
func TestTSReplay_TranslatesQuestionRequestsAndClientResults(t *testing.T) {
	var gotNative any
	var toNativeArgs []any
	var gotRequestID string
	toolResults := make(chan map[string]any, 4)
	started := make(chan *bridgetest.Turn, 1)
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok",
		OnStart:      func(turn *bridgetest.Turn, _ map[string]any) { started <- turn },
		OnToolResult: func(f map[string]any) { toolResults <- f }})
	t.Cleanup(srv.Close)
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.MintBridgeToken = func(string) string { return "tok" }
		s.HarnessID = "test-acp"
		s.AskUserQuestions = &AskUserQuestionsSettings{
			RequestMethod: "test/ask",
			FromNativeRequest: func(nativeRequest any, nativeToolCall *ToolCall) *harness.ToolCallPart {
				gotNative = nativeRequest
				if nativeToolCall != nil {
					t.Errorf("nativeToolCall = %+v, want nil", nativeToolCall)
				}
				return &harness.ToolCallPart{ToolCallID: "question-1", ToolName: "askUserQuestions",
					Input:            `{"allowPartialAnswers":false,"questions":[{"id":"q1","question":"Framework?"}]}`,
					ProviderMetadata: harness.ProviderMetadata{"test": {"preserved": true}}}
			},
			ToNativeResponse: func(nativeRequest any, toolResult types.ToolResultContent) any {
				toNativeArgs = []any{nativeRequest, toolResult}
				return map[string]any{"native": toolResult}
			},
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SandboxSession: newFakeSandbox(srv), SessionWorkDir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	var mu sync.Mutex
	var events []harness.StreamPart
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("Ask."), Emit: func(p harness.StreamPart) {
		mu.Lock()
		events = append(events, p)
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	turn := <-started
	nativeRequest := map[string]any{"sessionId": "native-session", "toolCallId": "native-question-1"}
	gotRequestID = "request-1"
	turn.Emit(map[string]any{"type": "acp-question-request", "requestId": gotRequestID, "nativeRequest": nativeRequest})

	handled := <-toolResults
	if handled["toolCallId"] != "request-1" || !reflect.DeepEqual(handled["output"], map[string]any{"type": "handled", "toolCallId": "question-1"}) {
		t.Fatalf("handled reply = %v", handled)
	}
	if !reflect.DeepEqual(gotNative, nativeRequest) {
		t.Fatalf("fromNativeRequest got %v", gotNative)
	}
	deadline := time.Now().Add(2 * time.Second)
	var forwarded *harness.ToolCallPart
	for time.Now().Before(deadline) && forwarded == nil {
		mu.Lock()
		for _, e := range events {
			if tc, ok := e.(*harness.ToolCallPart); ok {
				forwarded = tc
			}
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	if forwarded == nil {
		t.Fatal("the translated askUserQuestions tool call was not forwarded")
	}
	if forwarded.ToolCallID != "question-1" || forwarded.ProviderExecuted || forwarded.ProviderMetadata["test"]["preserved"] != true ||
		!reflect.DeepEqual(forwarded.ProviderMetadata["test-acp"]["nativeRequest"], nativeRequest) {
		t.Fatalf("forwarded = %+v", forwarded)
	}

	result := types.ToolResultContent{ToolCallID: "question-1", ToolName: "askUserQuestions",
		Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]any{"action": "answered", "answers": map[string]any{"q1": map[string]any{"optionIds": []any{}}}}}}
	if err := control.SubmitToolResult(context.Background(), harness.ToolResultSubmission{ToolCallID: "question-1", Output: result.Output.Value, ToolResult: &result}); err != nil {
		t.Fatal(err)
	}
	reply := <-toolResults
	if reply["toolCallId"] != "question-1" {
		t.Fatalf("reply = %v", reply)
	}
	if len(toNativeArgs) != 2 || !reflect.DeepEqual(toNativeArgs[0], nativeRequest) {
		t.Fatalf("toNativeResponse args = %v", toNativeArgs)
	}
	out, _ := reply["output"].(map[string]any)
	tr, _ := out["native"].(map[string]any)
	if tr["type"] != "tool-result" || tr["toolCallId"] != "question-1" || tr["toolName"] != "askUserQuestions" {
		t.Fatalf("output = %v", reply["output"])
	}
	if reply["toolResult"] == nil {
		t.Fatalf("toolResult missing from %v", reply)
	}
}
