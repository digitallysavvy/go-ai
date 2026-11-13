package mistral

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// TS withoutTrailingSlash(baseURL): a trailing slash must not produce "//".
func TestBaseURLTrailingSlashTrimmed(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/"})
	if _, err := p.Client().Do(context.Background(), internalhttp.Request{Method: http.MethodPost, Path: "/chat/completions", Body: map[string]interface{}{}}); err != nil {
		t.Fatal(err)
	}
	if path != "/chat/completions" {
		t.Fatalf("path = %q", path)
	}
}

// A TS-style baseURL that already carries /v1 must not double the segment.
func TestBaseURLWithVersionSegment(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	for _, base := range []string{srv.URL + "/v1", srv.URL + "/v1/", srv.URL} {
		p := New(Config{APIKey: "k", BaseURL: base})
		if _, err := p.Client().Do(context.Background(), internalhttp.Request{Method: http.MethodPost, Path: "/v1/chat/completions", Body: map[string]interface{}{}}); err != nil {
			t.Fatal(err)
		}
		if path != "/v1/chat/completions" {
			t.Fatalf("base %q: path = %q", base, path)
		}
	}
}
