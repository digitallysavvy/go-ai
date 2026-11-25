package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// tools r90 open item: TS writes stored.value[providerId] = {...candidate,
// access, refresh, expires, accountId} and JSON.stringify(stored.value,
// null, 2): existing keys keep their place. Go wrote sorted keys.
func TestR90integRefreshedAuthStoreKeyOrder(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "share", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "auth.json")
	expires := strconv.FormatInt(time.Now().Add(time.Minute).UnixMilli(), 10)
	original := `{"zeta":{"type":"api","key":"k"},"openai":{"refresh":"old-refresh","type":"oauth","expires":` + expires + `,"access":"old-access","accountId":"account-1"}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`))
	}))
	defer server.Close()
	_, err := readOpenCodeSubscriptionWithOptions(context.Background(), "openai", map[string]string{}, subscriptionReadOptions{
		HomeDirectory: home, HTTPClient: &http.Client{Transport: redirectTransport{to: server.URL}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range regexp.MustCompile(`"([A-Za-z_]+)":`).FindAllStringSubmatch(string(raw), -1) {
		got = append(got, m[1])
	}
	if want := "zeta type key openai refresh type expires access accountId"; strings.Join(got, " ") != want {
		t.Fatalf("keys = %v, want %s\n%s", got, want, raw)
	}
}
