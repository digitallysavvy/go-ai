package bfl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestBFLImageModel_PollCarriesSDKUserAgent(t *testing.T) {
	var pollUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			_, _ = w.Write([]byte(`{"id":"r","polling_url":"http://` + r.Host + `/poll"}`))
		case "/poll":
			pollUA = r.Header.Get("User-Agent")
			_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"http://` + r.Host + `/img.png"}}`))
		default:
			_, _ = w.Write([]byte("img"))
		}
	}))
	defer server.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), "flux-pro")
	if _, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pollUA, "ai-sdk-black-forest-labs") {
		t.Fatalf("poll user agent = %q", pollUA)
	}
}
