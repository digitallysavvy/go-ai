package quiverai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestBaseURLTrailingSlashIsStripped(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1/"}), ModelArrow11)
	_, _ = model.DoGenerate(t.Context(), &provider.ImageGenerateOptions{Prompt: "logo"})
	if path != "/v1/svgs/generations" {
		t.Fatalf("request path = %q, want /v1/svgs/generations", path)
	}
}
