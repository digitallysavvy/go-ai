package fx

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TS `sanitizeFxMcpToolNameSegment` walks UTF-8 bytes: a two-byte rune such
// as "é" (U+00E9) yields two underscores.
func TestSanitizeMCPToolNameSegmentCountsUTF8Bytes(t *testing.T) {
	cases := map[string]string{
		"":          "server",
		"ok-name_1": "ok-name_1",
		"a b":       "a_b",
		"é":         "__",
		"日":         "___",
		"a.é":       "a___",
	}
	for in, want := range cases {
		if got := sanitizeMCPToolNameSegment(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TS awaits extractChatGptAccountId without a catch, so a ChatGPT credential
// whose access token carries no account id fails the resolution.
func TestReadSubscriptions_ChatGPTTokenWithoutAccountIDErrors(t *testing.T) {
	home := createFxDirectory(t)
	writePrivateJSON(t, filepath.Join(home, ".fx", "chatgpt-auth.json"), createCredential("not-a-jwt", "", "account-1", 0))
	_, err := ReadSubscriptions(context.Background(), ReadSubscriptionsOptions{HomeDirectory: home})
	if err == nil || !strings.Contains(err.Error(), "fx ChatGPT access token does not contain an account ID.") {
		t.Fatalf("err = %v", err)
	}
}
