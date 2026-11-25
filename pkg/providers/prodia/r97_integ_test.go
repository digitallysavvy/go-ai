package prodia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

// TestR97integURLImageIsRawFetch (class sweep of provc M1, plain-fetch
// headers): TS fetches a URL image part with `fetchFn(url)` and reads
// arrayBuffer(), so the request has no SDK user-agent tag and the body is
// read whatever the status. The SSRF-validated download path stays.
func TestR97integURLImageIsRawFetch(t *testing.T) {
	prev := promptImageDownloadOptions
	promptImageDownloadOptions = func() fileutil.DownloadOptions {
		return fileutil.DownloadOptions{MaxSize: maxPromptImageBytes}
	}
	defer func() { promptImageDownloadOptions = prev }()
	var mu sync.Mutex
	var ua string
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ua = r.Header.Get("User-Agent")
		mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer img.Close()
	data, err := fetchImageURL(context.Background(), img.URL)
	if err != nil || string(data) != "not found" {
		t.Fatalf("data = %q, err = %v; want the body of the 404", data, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Contains(ua, "ai-sdk/") {
		t.Fatalf("User-Agent = %q, want no SDK tag", ua)
	}
}
