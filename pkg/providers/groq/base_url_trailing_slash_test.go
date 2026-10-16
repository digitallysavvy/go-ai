package groq

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
