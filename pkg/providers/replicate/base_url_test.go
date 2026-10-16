package replicate

import (
	"context"
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
	vm := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/v1/"}), "owner/model")
	_, _ = vm.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if path != "/v1/models/owner/model/predictions" {
		t.Fatalf("request path = %q", path)
	}
}
