package topaz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR97integCancelIsRawFetch (class sweep of provc M1, plain-fetch
// headers): TS cancelQuietly calls fetchImpl directly with the resolved
// headers only, so the DELETE has the provider's user-agent tag and the
// call's headers but no runtime tag.
func TestR97integCancelIsRawFetch(t *testing.T) {
	var mu sync.Mutex
	got := map[string]http.Header{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got[r.Method+" "+r.URL.Path] = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	prov := New(Config{APIKey: "k", BaseURL: srv.URL})
	NewImageModel(prov, ImageModelWonder35).cancelQuietly(context.Background(), "p1", map[string]string{"X-Call": "1"})
	NewVideoModel(prov, VideoModelStarlightPrecise26).cancelQuietly(context.Background(), "r1", map[string]string{"X-Call": "1"})
	mu.Lock()
	defer mu.Unlock()
	for _, key := range []string{"DELETE /image/v1/cancel/p1", "DELETE /video/r1"} {
		h, ok := got[key]
		if !ok {
			t.Errorf("%s: not sent (got %v)", key, got)
			continue
		}
		ua := h.Get("User-Agent")
		if !strings.Contains(ua, "ai-sdk-topaz/") || strings.Contains(ua, providerutils.RuntimeEnvironmentUserAgent()) || h.Get("X-Call") != "1" {
			t.Errorf("%s: headers = %v, want the provider tag and X-Call without the runtime tag", key, h)
		}
	}
}
