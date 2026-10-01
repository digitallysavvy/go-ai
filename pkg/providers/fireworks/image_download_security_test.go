package fireworks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Mirrors TS fireworks-image-model.test.ts download security cases
// (4be62c1 / aeda373): response-supplied image URLs are validated and only
// receive credentials when same-origin with the configured base URL.
func TestDownloadImage_SameOriginSendsCredentials(t *testing.T) {
	var gotAuth, gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Custom")
		w.Header().Set("Content-Type", "image/jpeg; charset=utf-8")
		_, _ = w.Write([]byte{1, 2, 3})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "secret", BaseURL: srv.URL, Headers: map[string]string{"X-Custom": "c"}})
	m := NewImageModel(p, "accounts/fireworks/models/flux-kontext-dev")
	res, err := m.downloadImage(context.Background(), srv.URL+"/image.jpg")
	if err != nil {
		t.Fatalf("downloadImage: %v", err)
	}
	if gotAuth != "Bearer secret" || gotCustom != "c" {
		t.Fatalf("same-origin credentials missing: auth=%q custom=%q", gotAuth, gotCustom)
	}
	if res.MimeType != "image/jpeg" || len(res.Image) != 3 {
		t.Fatalf("result = %+v", res)
	}
}

func TestDownloadImage_RejectsPrivateForeignURL(t *testing.T) {
	var hits atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer foreign.Close()

	p := New(Config{APIKey: "secret", BaseURL: "https://api.fireworks.ai/inference"})
	m := NewImageModel(p, "accounts/fireworks/models/flux-kontext-dev")
	for _, u := range []string{foreign.URL + "/image.png", "http://169.254.169.254/latest/meta-data"} {
		_, err := m.downloadImage(context.Background(), u)
		var de *providererrors.DownloadError
		if !errors.As(err, &de) {
			t.Fatalf("%s: err = %v, want DownloadError", u, err)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("private foreign URL was contacted")
	}
}

func TestDownloadImage_TrustedOriginRedirectToPrivateBlocked(t *testing.T) {
	var privateHits atomic.Int32
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		privateHits.Add(1)
	}))
	defer private.Close()
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, private.URL+"/secret", http.StatusFound)
	}))
	defer base.Close()

	p := New(Config{APIKey: "secret", BaseURL: base.URL})
	m := NewImageModel(p, "accounts/fireworks/models/flux-kontext-dev")
	if _, err := m.downloadImage(context.Background(), base.URL+"/image.png"); err == nil {
		t.Fatal("expected redirect to private foreign origin to be blocked")
	}
	if privateHits.Load() != 0 {
		t.Fatal("redirect target was contacted")
	}
}
