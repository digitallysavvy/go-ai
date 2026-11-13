package fireworks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestR99provbSameOriginDownloadUserAgent: TS downloads a same-origin
// result image with combineHeaders(config.headers, headers), and
// config.headers carries the ai-sdk-fireworks user-agent tag. Go built the
// headers by hand (key, custom headers, call headers) and sent no provider
// tag; a call header still wins over a configured one.
func TestR99provbSameOriginDownloadUserAgent(t *testing.T) {
	var ua, custom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, custom = r.Header.Get("User-Agent"), r.Header.Get("X-Custom")
		_, _ = w.Write([]byte{1})
	}))
	defer srv.Close()
	p := New(Config{APIKey: "secret", BaseURL: srv.URL, Headers: map[string]string{"X-Custom": "config"}})
	m := NewImageModel(p, "accounts/fireworks/models/flux-kontext-pro")
	if _, err := m.downloadImage(context.Background(), srv.URL+"/i.png", map[string]string{"x-custom": "call"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ua, "ai-sdk-fireworks/") {
		t.Errorf("User-Agent = %q, want the ai-sdk-fireworks tag first", ua)
	}
	if custom != "call" {
		t.Errorf("X-Custom = %q, want the call header", custom)
	}
}
