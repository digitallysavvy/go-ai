package minimax

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS withoutTrailingSlash(videoBaseURL): a trailing slash must not produce a
// "//v2/..." request path.
func TestVideoBaseURLTrailingSlashTrimmed(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	prov := New(Config{APIKey: "k", VideoBaseURL: srv.URL + "/"})
	model := newVideoModel(prov, ModelH3)
	_, _ = model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *minimaxDefaultCallOptions(map[string]interface{}{})})

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 || paths[0] != "/v2/video_generation" {
		t.Fatalf("paths = %v", paths)
	}
}
