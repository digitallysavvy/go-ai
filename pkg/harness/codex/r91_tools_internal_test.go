package codex

import (
	"context"
	"os"
	"testing"
	"time"
)

// harness-05 (TS codex-subscription.ts:316, typeof accountId === 'string'):
// an empty account_id still sends ChatGPT-Account-ID.
func TestR91toolsCodexEmptyAccountIDHeader(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	_ = os.Unsetenv("CODEX_HOME")
	writeAuthFile(t, home, map[string]any{
		"auth_mode": "chatgpt",
		"tokens":    map[string]any{"access_token": fakeJWT(t, time.Now().Add(24*time.Hour).Unix()), "refresh_token": "r", "account_id": ""},
	})
	sub, ok, err := resolveCodexSubscription(context.Background(), "")
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v", ok, err)
	}
	if v, has := sub.RequestHeaders["ChatGPT-Account-ID"]; !has || v != "" {
		t.Fatalf("RequestHeaders = %#v", sub.RequestHeaders)
	}
}
