package fx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// safeParseJSON rejects a __proto__ key, so the credential file is ignored.
func TestReadSubscriptionsRejectsForbiddenPrototypeKey(t *testing.T) {
	home := createFxDirectory(t)
	path := filepath.Join(home, ".fx", "grok-auth.json")
	body := `{"version":1,"access_token":"a","refresh_token":"r","expires_at_ms":9007199254740991,"account_id":"acc","__proto__":{"x":1}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSubscriptions(context.Background(), ReadSubscriptionsOptions{HomeDirectory: home})
	if err != nil || got != nil {
		t.Fatalf("got %v, %v; want nil, nil", got, err)
	}
	// Control: the same file without the key is accepted.
	body = strings.Replace(body, `,"__proto__":{"x":1}`, "", 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = ReadSubscriptions(context.Background(), ReadSubscriptionsOptions{HomeDirectory: home})
	if err != nil || got[GrokAccessTokenEnvVar] != "a" {
		t.Fatalf("control: got %v, %v", got, err)
	}
}

// JSON.stringify writes U+2028 raw.
func TestCredentialFileContentKeepsLineSeparatorsRaw(t *testing.T) {
	got := credentialFileContent("a", "r", 1, "x y")
	if !strings.Contains(got, "x y") {
		t.Fatalf("got %q", got)
	}
}
