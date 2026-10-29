package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// A URL input image without a full caller media type takes its media type
// from the URL path: a query string or fragment after the extension does
// not hide it. A caller-supplied full media type still wins.
func TestR66IntegratorVertexImageURLMediaTypeFromPath(t *testing.T) {
	var mu sync.Mutex
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Contents []struct {
				Parts []map[string]map[string]interface{} `json:"parts"`
			} `json:"contents"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		mu.Lock()
		for _, p := range body.Contents[0].Parts {
			if fd, ok := p["fileData"]; ok {
				got = append(got, fd["fileUri"].(string)+" "+fd["mimeType"].(string))
			}
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1n"}}]}}]}`)) //nolint:errcheck
	}))
	defer server.Close()

	prov, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "t", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewImageModel(prov, "gemini-2.5-flash-image").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "edit",
		Files: []provider.ImageFile{
			{Type: "url", URL: "https://x/y.png?sig=1"},
			{Type: "url", URL: "gs://b/y.webp#v"},
			{Type: "url", URL: "https://x/y.GIF?a=b#c", MediaType: "image/*"},
			{Type: "url", URL: "https://x/y?f=z.png"},
			{Type: "url", URL: "https://x/z.png?q", MediaType: "image"},
			{Type: "url", URL: "https://x/y.png?sig=1", MediaType: "image/heic"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://x/y.png?sig=1 image/png",
		"gs://b/y.webp#v image/webp",
		"https://x/y.GIF?a=b#c image/gif",
		"https://x/y?f=z.png image/jpeg",
		"https://x/z.png?q image/png",
		"https://x/y.png?sig=1 image/heic",
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != len(want) {
		t.Fatalf("fileData parts = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("part %d = %q, want %q", i, got[i], want[i])
		}
	}
}
