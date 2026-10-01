package cursor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Mirrors "does not inspect subscriptions for Gateway auth".
func TestResolveSubscriptionEnvironment_Gateway(t *testing.T) {
	env := map[string]string{"AI_GATEWAY_API_KEY": "gateway"}
	got, err := ResolveSubscriptionEnvironment(context.Background(), harness.AuthMode(harness.AuthModeAIGateway), env)
	if err != nil {
		t.Fatal(err)
	}
	if got["AI_GATEWAY_API_KEY"] != "gateway" {
		t.Errorf("got %v", got)
	}
}

// Mirrors "prefers CURSOR_API_KEY over native storage".
func TestResolveSubscriptionEnvironment_PrefersDirectCredential(t *testing.T) {
	env := map[string]string{"CURSOR_API_KEY": "environment-key"}
	got, err := ResolveSubscriptionEnvironment(context.Background(), harness.AuthMode(harness.AuthModeDirect), env)
	if err != nil {
		t.Fatal(err)
	}
	if got["CURSOR_API_KEY"] != "environment-key" {
		t.Errorf("got %v", got)
	}
}

// Mirrors "reads a file-backed browser access token".
func TestReadSubscription_FileBacked(t *testing.T) {
	homeDirectory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDirectory, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{
		"accessToken":  "opaque-access-token",
		"refreshToken": "stored-but-not-forwarded",
	})
	if err := os.WriteFile(filepath.Join(homeDirectory, ".cursor", "auth.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	token, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{HomeDirectory: homeDirectory, GOOS: "darwin"})
	if err != nil {
		t.Fatal(err)
	}
	if token != "opaque-access-token" {
		t.Errorf("token = %q", token)
	}
}

// Mirrors "rejects an expiring JWT instead of inventing a refresh endpoint".
func TestReadSubscription_ExpiringJWT(t *testing.T) {
	homeDirectory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDirectory, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]int64{"exp": time.Now().Add(60 * time.Second).Unix()})
	accessToken := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	data, _ := json.Marshal(map[string]string{"accessToken": accessToken, "refreshToken": "refresh-token"})
	if err := os.WriteFile(filepath.Join(homeDirectory, ".cursor", "auth.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadSubscription(context.Background(), ReadSubscriptionOptions{HomeDirectory: homeDirectory, GOOS: "darwin"})
	if err == nil || !strings.Contains(err.Error(), "Run Cursor login again.") {
		t.Errorf("err = %v", err)
	}
}

// Mirrors "uses each native platform path".
func TestResolveAuthPath(t *testing.T) {
	if got := ResolveAuthPath(map[string]string{}, "/home/me", "linux"); got != "/home/me/.config/cursor/auth.json" {
		t.Errorf("linux path = %q", got)
	}
	got := ResolveAuthPath(map[string]string{"APPDATA": `C:\Users\me\AppData\Roaming`}, `C:\Users\me`, "windows")
	if !strings.Contains(got, "Cursor/auth.json") && !strings.Contains(got, `Cursor\auth.json`) {
		t.Errorf("windows path = %q", got)
	}
}
