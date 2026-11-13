package luma

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS #23679: the generation ID is one encoded path segment.
func TestDoGenerate_EncodesGenerationIDAsSinglePathSegment(t *testing.T) {
	const rawID = "abc/../../internal"
	var pollURI string
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/dream-machine/v1/generations/image", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": rawID, "state": "queued"})
	})
	mux.HandleFunc("/image.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF})
	})
	mux.HandleFunc("/dream-machine/", func(w http.ResponseWriter, r *http.Request) {
		pollURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": rawID, "state": "completed",
			"assets": map[string]interface{}{"image": serverURL + "/image.jpg"},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL
	model := NewImageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelPhoton1)
	// The final image download is SSRF-guarded and may refuse the loopback
	// fixture; only the poll request matters here.
	_, _ = model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	want := "/dream-machine/v1/generations/" + providerutils.EncodePathSegment(rawID)
	if pollURI != want {
		t.Fatalf("poll URI = %q, want %q", pollURI, want)
	}
}
