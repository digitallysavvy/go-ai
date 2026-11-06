package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// safeParseJSON rejects a __proto__ key: the store and the direct-access
// response are "invalid JSON".
func TestOpenCodeStoreRejectsForbiddenPrototypeKey(t *testing.T) {
	content := `{"opencode-go":{"type":"api","key":"k"},"__proto__":{}}`
	sub, err := readOpenCodeSubscription(context.Background(), "opencode-go", map[string]string{"OPENCODE_AUTH_CONTENT": content})
	if err != nil || sub != nil {
		t.Fatalf("sub=%v err=%v", sub, err)
	}
	// Control without the key.
	sub, err = readOpenCodeSubscription(context.Background(), "opencode-go", map[string]string{"OPENCODE_AUTH_CONTENT": `{"opencode-go":{"type":"api","key":"k"}}`})
	if err != nil || sub == nil || sub.AccessToken != "k" {
		t.Fatalf("control sub=%v err=%v", sub, err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode", "auth.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sub, err = readOpenCodeSubscription(context.Background(), "opencode-go", map[string]string{"XDG_DATA_HOME": dir})
	if err != nil || sub != nil {
		t.Fatalf("file sub=%v err=%v", sub, err)
	}
}

func TestDirectAccessRejectsForbiddenPrototypeKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"t","headers":{},"__proto__":{}}`))
	}))
	defer server.Close()
	_, err := requestOpenCodeGitLabDirectAccess(context.Background(), server.Client(), "tok", server.URL, "")
	if err == nil || !strings.Contains(err.Error(), "returned invalid JSON") {
		t.Fatalf("err = %v", err)
	}
}

// JSON.stringify(value, null, 2) does not HTML-escape.
func TestWriteAuthStoreHasNoHTMLEscapes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := writeAuthStore(path, map[string]any{"a": map[string]any{"e": "<x&y> "}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "{\n  \"a\": {\n    \"e\": \"<x&y> \"\n  }\n}\n"
	if string(data) != want {
		t.Fatalf("got %q want %q", data, want)
	}
}

// A compaction failure rejects with the raw bridge error value: a wire object
// keeps its wire key order in the message text.
func TestDoCompact_ErrorObjectKeepsWireKeyOrder(t *testing.T) {
	const token = "compact-err-token"
	srv := newServer(t, token, func(turn *bridgetest.Turn, _ map[string]any) {
		turn.Emit(map[string]any{"type": "error", "error": json.RawMessage(`{"zeta":1,"alpha":2}`)})
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return token }})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "compact-err", SessionWorkDir: "/vercel/sandbox/compact-err", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	err = sess.DoCompact(context.Background(), "")
	if err == nil || err.Error() != `{"zeta":1,"alpha":2}` {
		t.Fatalf("err = %v", err)
	}
}
