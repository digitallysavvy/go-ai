package openai

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ported from openai-compatible-image-model.test.ts "URL image responses"
// (TS #23403, generations endpoint).
func TestCompatImageURLResponses(t *testing.T) {
	png := []byte{137, 80, 78, 71}
	var mu sync.Mutex
	var downloadAuth, downloadKey string
	downloads := 0
	var respBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/images/generations" {
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			b := respBody
			mu.Unlock()
			_, _ = w.Write([]byte(b))
			return
		}
		mu.Lock()
		downloads++
		downloadAuth, downloadKey = r.Header.Get("Authorization"), r.Header.Get("x-api-key")
		mu.Unlock()
		_, _ = w.Write(png)
	}))
	defer srv.Close()
	gen := func(body string) ([][]byte, []string, error) {
		mu.Lock()
		respBody, downloads = body, 0
		mu.Unlock()
		p := New(Config{APIKey: "test-key", Name: "acme", BaseURL: srv.URL + "/v1", OpenAICompatible: true})
		res, err := NewImageModel(p, "m").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt: "x", Headers: map[string]string{"x-api-key": "custom"},
		})
		if err != nil {
			return nil, nil, err
		}
		return res.Images, res.Base64Images, nil
	}

	t.Run("downloads same-origin URL-only images with provider and call headers", func(t *testing.T) {
		imgs, _, err := gen(`{"data":[{"b64_json":null,"url":"` + srv.URL + `/img.png"}]}`)
		if err != nil || len(imgs) != 1 || !bytes.Equal(imgs[0], png) {
			t.Fatalf("imgs=%v err=%v", imgs, err)
		}
		mu.Lock()
		defer mu.Unlock()
		if downloads != 1 || downloadAuth != "Bearer test-key" || downloadKey != "custom" {
			t.Fatalf("downloads=%d auth=%q key=%q", downloads, downloadAuth, downloadKey)
		}
	})
	t.Run("preserves order in mixed responses", func(t *testing.T) {
		imgs, _, err := gen(`{"data":[{"url":"` + srv.URL + `/img.png"},{"b64_json":"AQID","url":"https://cdn.example.com/unused.png"},{"b64_json":"BAUG","url":null}]}`)
		if err != nil || len(imgs) != 3 || !bytes.Equal(imgs[0], png) || !bytes.Equal(imgs[1], []byte{1, 2, 3}) || !bytes.Equal(imgs[2], []byte{4, 5, 6}) {
			t.Fatalf("imgs=%v err=%v", imgs, err)
		}
		mu.Lock()
		defer mu.Unlock()
		if downloads != 1 {
			t.Fatalf("downloads=%d", downloads)
		}
	})
	t.Run("base64 with url does not download", func(t *testing.T) {
		_, b64, err := gen(`{"data":[{"b64_json":"AQID","url":"` + srv.URL + `/img.png"}]}`)
		if err != nil || len(b64) != 1 || b64[0] != "AQID" {
			t.Fatalf("b64=%v err=%v", b64, err)
		}
		mu.Lock()
		defer mu.Unlock()
		if downloads != 0 {
			t.Fatalf("downloads=%d", downloads)
		}
	})
	t.Run("rejects neither", func(t *testing.T) {
		for _, body := range []string{`{"data":[{}]}`, `{"data":[{"b64_json":null,"url":null}]}`} {
			if _, _, err := gen(body); err == nil {
				t.Fatalf("expected error for %s", body)
			}
			mu.Lock()
			d := downloads
			mu.Unlock()
			if d != 0 {
				t.Fatalf("downloads=%d", d)
			}
		}
	})
	t.Run("propagates download failure", func(t *testing.T) {
		bad := httptest.NewServer(http.NotFoundHandler())
		defer bad.Close()
		// Cross-origin URL is SSRF-validated: loopback is refused.
		if _, _, err := gen(`{"data":[{"url":"` + bad.URL + `/x.png"}]}`); err == nil {
			t.Fatal("expected error")
		}
	})
}

// TS fetchUntrustedUrl: provider credentials reach only the configured API
// origin; any other host gets the non-credential allowlist.
func TestCompatDownloadHeadersWithholdCredentialsFromOtherOrigins(t *testing.T) {
	p := New(Config{APIKey: "test-key", Name: "acme", BaseURL: "https://api.example.com/v1", OpenAICompatible: true})
	m := NewImageModel(p, "m")
	call := map[string]string{"x-api-key": "custom", "Traceparent": "00-abc", "Content-Type": "application/json"}

	same := m.compatDownloadHeaders("https://api.example.com/files/a.png", call)
	if same["authorization"] != "Bearer test-key" || same["x-api-key"] != "custom" {
		t.Fatalf("same-origin headers = %v", same)
	}
	other := m.compatDownloadHeaders("https://cdn.example.com/a.png", call)
	if _, ok := other["authorization"]; ok {
		t.Fatalf("authorization leaked to another origin: %v", other)
	}
	if _, ok := other["x-api-key"]; ok {
		t.Fatalf("x-api-key leaked to another origin: %v", other)
	}
	if other["Traceparent"] != "00-abc" {
		t.Fatalf("traceparent should be forwarded: %v", other)
	}
	if _, ok := other["Content-Type"]; ok {
		t.Fatalf("content-type should not be forwarded: %v", other)
	}
}
