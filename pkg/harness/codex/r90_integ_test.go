package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// tools r90 open item: TS writes the refreshed store as JSON.stringify({
// ...stored.value, last_refresh, tokens: {...tokens, access_token,
// refresh_token}}, null, 2): every existing key keeps its place, new keys
// are appended. Go wrote sorted keys.
func TestR90integRefreshedAuthFileKeyOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	_ = os.Unsetenv("CODEX_HOME")
	refreshed := fakeJWT(t, time.Now().Add(24*time.Hour).Unix())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": refreshed, "refresh_token": "new-refresh", "expires_in": 3600})
	}))
	t.Cleanup(srv.Close)
	old := openaiTokenURL
	openaiTokenURL = srv.URL
	t.Cleanup(func() { openaiTokenURL = old })

	stale := fakeJWT(t, time.Now().Add(time.Minute).Unix())
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "auth.json")
	original := `{"tokens":{"refresh_token":"old","id_token":"i","access_token":"` + stale + `"},"zeta":1,"auth_mode":"chatgpt"}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readCodexSubscription(context.Background()); !ok {
		t.Fatal("expected a refreshed credential")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	keys := regexp.MustCompile(`"([a-z_]+)":`).FindAllStringSubmatch(string(raw), -1)
	var got []string
	for _, k := range keys {
		got = append(got, k[1])
	}
	want := []string{"tokens", "refresh_token", "id_token", "access_token", "zeta", "auth_mode", "last_refresh"}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v\n%s", got, want, raw)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %v, want %v\n%s", got, want, raw)
		}
	}
}
