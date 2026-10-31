package fx_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx"
)

// TestV052APICompiles references every identifier pkg/harness/fx exported
// at go-ai v0.5.2 (a0f9d3f9) with its v0.5.2 type. It fails to compile if
// one is removed or retyped.
func TestV052APICompiles(t *testing.T) {
	//nolint:staticcheck // the explicit v0.5.2 types are what this test checks
	var (
		_ string = fx.ClientAppName
		_ string = fx.ClientAppVersion
		_ string = fx.HarnessID
		_ string = fx.ChatGPTAccessTokenEnvVar
		_ string = fx.ChatGPTAccountIDEnvVar
		_ string = fx.GrokAccessTokenEnvVar
		_ string = fx.GrokAccountIDEnvVar

		_ []string                       = fx.SubscriptionEnvironmentVariables
		_ map[string]harness.BuiltinTool = fx.BuiltinTools

		_ func(...fx.Settings) (harness.Harness, error)                                                   = fx.CreateFx
		_ func(fx.Settings) acp.Settings                                                                  = fx.BuildConfig
		_ func(env, sandboxEnv, headers map[string]string, supplied bool) []harness.RequestTransformation = fx.CredentialBrokering
		_ func(context.Context, fx.ResolveSubscriptionEnvironmentOptions) (map[string]string, error)      = fx.ResolveSubscriptionEnvironment
		_ func(context.Context, fx.ReadSubscriptionsOptions) (map[string]string, error)                   = fx.ReadSubscriptions
		_ func(env, sandboxEnv map[string]string, brokering bool) []acp.AuthenticationFile                = fx.CreateSubscriptionAuthenticationFiles
		_ func(env, sandboxEnv map[string]string) []fx.RequestCredential                                  = fx.GetSubscriptionRequestCredentials
	)
	var auth fx.AuthenticationMode = harness.Authentication{} //nolint:staticcheck // checks the v0.5.2 alias
	_ = fx.Settings{
		Auth:                 auth,
		CredentialForwarding: harness.CredentialForwarding(nil),
		Port:                 new(int),
		PortEndpoint:         &harness.PortEndpoint{},
		StartupTimeoutMS:     new(int),
		Reconnect:            &bridge.ReconnectOptions{},
		MCPServers:           map[string]any{},
		MintBridgeToken:      harness.MintBridgeTokenCallback(nil),
	}
	_ = fx.Credential{AccessToken: "", RefreshToken: "", ExpiresAt: int64(0), AccountID: ""}
	_ = fx.RequestCredential{Provider: "", AccessToken: "", SandboxAccessToken: ""}
	_ = fx.ResolveSubscriptionEnvironmentOptions{Auth: auth, Env: map[string]string{}, HomeDirectory: "", HTTPClient: (*http.Client)(nil)}
	_ = fx.ReadSubscriptionsOptions{HomeDirectory: "", HTTPClient: (*http.Client)(nil)}
}

// TestDefaultRuntimeIsSandboxACP pins the v0.5.x default: CreateFx without a
// Runtime keeps the sandbox-ACP adapter and its v0.5.x built-in tools.
func TestDefaultRuntimeIsSandboxACP(t *testing.T) {
	h, err := fx.CreateFx()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.BuiltinTools()) != len(fx.BuiltinTools) {
		t.Errorf("default builtin tools = %d, want the v0.5.x set (%d)", len(h.BuiltinTools()), len(fx.BuiltinTools))
	}
	if _, ok := h.BuiltinTools()["read_file"]; !ok {
		t.Error("default runtime lost the v0.5.x read_file tool")
	}
	if _, ok := h.(harness.BootstrapProvider); !ok {
		t.Error("default runtime lost its sandbox bootstrap")
	}
	libfx, err := fx.CreateFx(fx.Settings{Runtime: fx.RuntimeLibfx})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := libfx.BuiltinTools()["read"]; !ok || len(libfx.BuiltinTools()) != len(fx.LibfxBuiltinTools) {
		t.Error("libfx runtime does not expose LibfxBuiltinTools")
	}
}
