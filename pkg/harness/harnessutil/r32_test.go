package harnessutil

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func refreshWith(t *testing.T, format, refreshToken, response string) (string, error) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		_, _ = w.Write([]byte(response))
	}))
	defer srv.Close()
	_, err := RefreshOAuthAccessToken(context.Background(), RefreshOAuthAccessTokenOptions{
		TokenURL: srv.URL, ClientID: "c", RefreshToken: refreshToken, RequestFormat: format,
	})
	return got, err
}

// URLSearchParams leaves only *-._ unescaped: `*` stays, `~` becomes %7E.
func TestRefreshFormBodyMatchesURLSearchParams(t *testing.T) {
	got, _ := refreshWith(t, "form", "a b*~&é", `{}`)
	want := "grant_type=refresh_token&client_id=c&refresh_token=a+b*%7E%26%C3%A9"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

// JSON.stringify does not HTML-escape.
func TestRefreshJSONBodyHasNoHTMLEscapes(t *testing.T) {
	got, _ := refreshWith(t, "json", "a<b&c ", `{}`)
	want := "{\"grant_type\":\"refresh_token\",\"client_id\":\"c\",\"refresh_token\":\"a<b&c \"}"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// safeParseJSON rejects __proto__, so the response is "invalid JSON".
func TestRefreshRejectsForbiddenPrototypeResponse(t *testing.T) {
	_, err := refreshWith(t, "form", "r", `{"access_token":"a","expires_in":1,"__proto__":{}}`)
	if err == nil || !strings.Contains(err.Error(), "returned invalid JSON") {
		t.Fatalf("err = %v", err)
	}
}

func TestJSONStringMatchesJSONStringify(t *testing.T) {
	if got := jsonString("a<b "); got != "\"a<b \"" {
		t.Fatalf("got %q", got)
	}
}
