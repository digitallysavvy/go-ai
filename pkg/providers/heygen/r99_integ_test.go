package heygen

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// cr-r99-provg-to-integrator: the status poll is TS getFromApi, which sends
// the SDK client user agent (provider tag, runtime tag) without download()'s
// ai-sdk/<version> tag.
func TestR99integStatusPollUserAgent(t *testing.T) {
	var mu sync.Mutex
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ua = r.Header.Get("User-Agent")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, r89provgStatusBody)
	}))
	defer srv.Close()
	m := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "heygen-video-1")
	_, _ = m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
	want := "ai-sdk-heygen/" + version.Version + " " + providerutils.RuntimeEnvironmentUserAgent()
	mu.Lock()
	defer mu.Unlock()
	if ua != want {
		t.Fatalf("User-Agent = %q, want %q", ua, want)
	}
}
