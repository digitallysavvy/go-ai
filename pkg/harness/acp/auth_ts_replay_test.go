package acp

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

var tsClientApp = ClientApp{Name: "ai-sdk-harness-acp", Version: "0.0.0-test"}

var tsGatewayEnv = map[string]any{
	"PROVIDER_API_KEY":  map[string]any{"$source": "gateway-api-key"},
	"PROVIDER_BASE_URL": map[string]any{"$source": "gateway-base-url"},
}

func tsResolve(t *testing.T, auth harness.Authentication, env map[string]string) (resolvedProviderAuthentication, error) {
	t.Helper()
	pa := &ProviderAuthentication{GatewayEnv: tsGatewayEnv}
	return resolveProviderAuthentication(auth, pa, tsClientApp, env, nil)
}

func clientAppEnv() map[string]string {
	return map[string]string{"AI_SDK_ACP_CLIENT_APP_NAME": tsClientApp.Name, "AI_SDK_ACP_CLIENT_APP_VERSION": tsClientApp.Version}
}

func withClientApp(m map[string]string) map[string]string {
	for k, v := range clientAppEnv() {
		m[k] = v
	}
	return m
}

// Replays TS acp-auth.test.ts `resolveACPProviderAuthentication`.
func TestTSReplay_ResolveProviderAuthentication(t *testing.T) {
	cases := []struct {
		name     string
		auth     harness.Authentication
		env      map[string]string
		wantType string
		wantEnv  map[string]string
		wantErr  string
	}{
		{"selects AI Gateway automatically for AI_GATEWAY_API_KEY", harness.Authentication{},
			map[string]string{"AI_GATEWAY_API_KEY": "gateway-key", "AI_GATEWAY_BASE_URL": "https://gateway.example"}, "ai-gateway",
			withClientApp(map[string]string{"AI_SDK_ACP_GATEWAY_API_KEY": "gateway-key", "AI_SDK_ACP_GATEWAY_BASE_URL": "https://gateway.example"}), ""},
		{"selects AI Gateway automatically for VERCEL_OIDC_TOKEN", harness.Authentication{},
			map[string]string{"VERCEL_OIDC_TOKEN": "oidc-token"}, "ai-gateway",
			withClientApp(map[string]string{"AI_SDK_ACP_GATEWAY_API_KEY": "oidc-token", "AI_SDK_ACP_GATEWAY_BASE_URL": "https://ai-gateway.vercel.sh"}), ""},
		{"selects direct authentication automatically without Gateway credentials", harness.Authentication{},
			map[string]string{"DIRECT_PROVIDER_KEY": "direct-key"}, "direct", clientAppEnv(), ""},
		{"keeps explicit direct authentication when Gateway credentials are ambient", harness.Authentication{Mode: harness.AuthModeDirect},
			map[string]string{"AI_GATEWAY_API_KEY": "ambient-gateway-key"}, "direct", clientAppEnv(), ""},
		{"uses the Gateway base URL from the environment", harness.Authentication{Mode: harness.AuthModeAIGateway},
			map[string]string{"AI_GATEWAY_API_KEY": "ambient-gateway-key", "AI_GATEWAY_BASE_URL": "https://configured.example"}, "ai-gateway",
			withClientApp(map[string]string{"AI_SDK_ACP_GATEWAY_API_KEY": "ambient-gateway-key", "AI_SDK_ACP_GATEWAY_BASE_URL": "https://configured.example"}), ""},
		{"uses a supplied authentication environment without reading the host environment",
			harness.AuthEnvironment(map[string]string{"AI_GATEWAY_API_KEY": "explicit-gateway-key", "AI_GATEWAY_BASE_URL": "https://explicit.example"}),
			map[string]string{"AI_GATEWAY_API_KEY": "ambient-gateway-key"}, "ai-gateway",
			withClientApp(map[string]string{"AI_SDK_ACP_GATEWAY_API_KEY": "explicit-gateway-key", "AI_SDK_ACP_GATEWAY_BASE_URL": "https://explicit.example"}), ""},
		{"fails explicit Gateway selection without credentials", harness.Authentication{Mode: harness.AuthModeAIGateway}, map[string]string{}, "", nil,
			"neither AI_GATEWAY_API_KEY nor VERCEL_OIDC_TOKEN is set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tsResolve(t, tc.auth, tc.env)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Type != tc.wantType || !reflect.DeepEqual(got.Env, tc.wantEnv) {
				t.Fatalf("got type %q env %v, want %q %v", got.Type, got.Env, tc.wantType, tc.wantEnv)
			}
			if tc.wantType == "ai-gateway" && !reflect.DeepEqual(got.GatewayEnv, tsGatewayEnv) {
				t.Fatalf("profile env = %v", got.GatewayEnv)
			}
		})
	}
	t.Run("transports the client app without provider authentication configuration", func(t *testing.T) {
		got, err := resolveProviderAuthentication(harness.Authentication{}, nil, tsClientApp, map[string]string{}, nil)
		if err != nil || got.Type != "" || !reflect.DeepEqual(got.Env, clientAppEnv()) {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
}

// Replays TS `resolveACPProviderAuthenticationCompatibility` and the pinning
// behavior of `resolveACPProviderAuthentication` with a compatibility record.
func TestTSReplay_ProviderAuthenticationCompatibility(t *testing.T) {
	pa := &ProviderAuthentication{GatewayEnv: tsGatewayEnv}
	compat := func(auth harness.Authentication, env map[string]string) *providerAuthenticationCompatibility {
		return resolveProviderAuthenticationCompatibility(auth, pa, env)
	}
	t.Run("records the credential source and base URL, not the secret", func(t *testing.T) {
		first := compat(harness.Authentication{}, map[string]string{"AI_GATEWAY_API_KEY": "first-secret", "AI_GATEWAY_BASE_URL": "https://gateway.example"})
		rotated := compat(harness.Authentication{}, map[string]string{"AI_GATEWAY_API_KEY": "rotated-secret", "AI_GATEWAY_BASE_URL": "https://gateway.example"})
		oidc := compat(harness.Authentication{}, map[string]string{"VERCEL_OIDC_TOKEN": "oidc-secret", "AI_GATEWAY_BASE_URL": "https://gateway.example"})
		if !reflect.DeepEqual(first, rotated) {
			t.Fatalf("secret rotation changed the compatibility record: %+v vs %+v", first, rotated)
		}
		if first.Type != "ai-gateway" || first.CredentialSource != gatewaySourceAPIKey || first.BaseURL != "https://gateway.example" {
			t.Fatalf("first = %+v", first)
		}
		if oidc.CredentialSource != gatewaySourceOIDCToken {
			t.Fatalf("oidc = %+v", oidc)
		}
		for _, c := range []*providerAuthenticationCompatibility{first, oidc} {
			b, _ := json.Marshal(c)
			if strings.Contains(string(b), "first-secret") || strings.Contains(string(b), "oidc-secret") {
				t.Fatalf("record leaked a secret: %s", b)
			}
		}
	})
	t.Run("supplied credentials are excluded from the identity", func(t *testing.T) {
		first := compat(harness.AuthEnvironment(map[string]string{"AI_GATEWAY_API_KEY": "first-secret", "AI_GATEWAY_BASE_URL": "https://gateway.example"}), nil)
		rotated := compat(harness.AuthEnvironment(map[string]string{"AI_GATEWAY_API_KEY": "rotated-secret", "AI_GATEWAY_BASE_URL": "https://gateway.example"}), nil)
		if !reflect.DeepEqual(first, rotated) || first.Mode != "auto" || first.CredentialSource != gatewaySourceAPIKey || first.BaseURL != "https://gateway.example" {
			t.Fatalf("first = %+v, rotated = %+v", first, rotated)
		}
	})
	t.Run("pins selection, source and base URL while allowing secret rotation", func(t *testing.T) {
		c := compat(harness.Authentication{}, map[string]string{"AI_GATEWAY_API_KEY": "first-secret", "AI_GATEWAY_BASE_URL": "https://first.example"})
		got, err := resolveProviderAuthentication(harness.Authentication{}, pa, tsClientApp,
			map[string]string{"AI_GATEWAY_API_KEY": "rotated-secret", "AI_GATEWAY_BASE_URL": "https://changed.example"}, c)
		if err != nil {
			t.Fatal(err)
		}
		want := withClientApp(map[string]string{"AI_SDK_ACP_GATEWAY_API_KEY": "rotated-secret", "AI_SDK_ACP_GATEWAY_BASE_URL": "https://first.example"})
		if got.Type != "ai-gateway" || !reflect.DeepEqual(got.Env, want) {
			t.Fatalf("got %+v", got)
		}
		_, err = resolveProviderAuthentication(harness.Authentication{}, pa, tsClientApp, map[string]string{"VERCEL_OIDC_TOKEN": "different-source-secret"}, c)
		if err == nil || !strings.Contains(err.Error(), "AI Gateway authentication was selected") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("does not change an auto-direct identity when credentials appear later", func(t *testing.T) {
		c := compat(harness.Authentication{}, map[string]string{})
		got, err := resolveProviderAuthentication(harness.Authentication{}, pa, tsClientApp, map[string]string{"AI_GATEWAY_API_KEY": "late-secret"}, c)
		if err != nil || got.Type != "direct" || !reflect.DeepEqual(got.Env, clientAppEnv()) {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
}

// Replays TS `createACPAuthenticationProfileIdentity`.
func TestTSReplay_AuthenticationProfileIdentity(t *testing.T) {
	env := map[string]any{"PROVIDER_API_KEY": map[string]any{"$source": "gateway-api-key"}, "AUTHENTICATED_HEADER": "authenticated-header-secret"}
	pa := &ProviderAuthentication{GatewayEnv: env}
	c := resolveProviderAuthenticationCompatibility(harness.Authentication{Mode: harness.AuthModeAIGateway}, pa, map[string]string{"VERCEL_OIDC_TOKEN": "resolved-oidc-secret"})
	id := createAuthenticationProfileIdentity(&Authentication{MethodID: "api-key", Meta: map[string]any{"token": "acp-authentication-secret"},
		ClientCapabilities: map[string]any{"headers": map[string]any{"Authorization": "client-capability-secret"}}}, c)
	b, _ := json.Marshal(id)
	s := string(b)
	if id.ACPMethodID != "api-key" || id.ProviderKind != "ai-gateway" || id.ProviderMode != "ai-gateway" || id.GatewayCredentialSource != "VERCEL_OIDC_TOKEN" || len(id.Digest) != 64 {
		t.Fatalf("identity = %+v", id)
	}
	for _, secret := range []string{"resolved-oidc-secret", "authenticated-header-secret", "acp-authentication-secret", "client-capability-secret", "Authorization"} {
		if strings.Contains(s, secret) {
			t.Fatalf("identity leaked %q: %s", secret, s)
		}
	}
	// The digest is stable across secret rotation at the same source.
	pa2 := &ProviderAuthentication{GatewayEnv: tsGatewayEnv}
	a := createAuthenticationProfileIdentity(&Authentication{MethodID: "api-key"}, resolveProviderAuthenticationCompatibility(harness.Authentication{}, pa2, map[string]string{"AI_GATEWAY_API_KEY": "first-secret"}))
	b2 := createAuthenticationProfileIdentity(&Authentication{MethodID: "api-key"}, resolveProviderAuthenticationCompatibility(harness.Authentication{}, pa2, map[string]string{"AI_GATEWAY_API_KEY": "rotated-secret"}))
	if !reflect.DeepEqual(a, b2) {
		t.Fatalf("digest changed across rotation: %+v vs %+v", a, b2)
	}
}
