package fireworks

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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

// TestR99provbSyncImageEmptyBody: TS createBinaryResponseHandler fails only
// a null body ("Response body is empty": fetch gives 204 / 205 one); a 200
// with an empty body is an empty image. Go failed the empty 200.
func TestR99provbSyncImageEmptyBody(t *testing.T) {
	for _, tc := range []struct {
		status  int
		wantErr bool
	}{{200, false}, {204, true}, {205, true}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}))
		m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "accounts/fireworks/models/flux-1-dev-fp8")
		res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
		srv.Close()
		if tc.wantErr {
			if err == nil || !strings.Contains(err.Error(), "Response body is empty") {
				t.Errorf("%d: err = %v, want Response body is empty", tc.status, err)
			}
			continue
		}
		if err != nil || len(res.Images) != 1 || len(res.Images[0]) != 0 {
			t.Errorf("%d: result = %+v, %v, want one empty image", tc.status, res, err)
		}
	}
}
