package googlevertex

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r90provgImageServers starts an API server answering /images/generations
// with one image URL (built by imageURL from the API and foreign server
// URLs), and a foreign server that serves image bytes and records each
// request's headers.
type r90provgImageServers struct {
	mu         sync.Mutex
	apiHits    []http.Header
	foreignHit []http.Header
	api        *httptest.Server
	foreign    *httptest.Server
}

func r90provgStartImageServers(t *testing.T, imageURL func(api, foreign string) string) *r90provgImageServers {
	t.Helper()
	s := &r90provgImageServers{}
	s.foreign = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.foreignHit = append(s.foreignHit, r.Header.Clone())
		s.mu.Unlock()
		_, _ = w.Write([]byte{1, 2, 3})
	}))
	t.Cleanup(s.foreign.Close)
	s.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.apiHits = append(s.apiHits, r.Header.Clone())
		s.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/images/generations"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"url":"`+imageURL(s.api.URL, s.foreign.URL)+`"}]}`)
		case r.URL.Path == "/v1/redirect.png":
			w.Header().Set("Location", s.foreign.URL+"/image.png")
			w.WriteHeader(http.StatusFound)
		default:
			_, _ = w.Write([]byte{4, 5, 6})
		}
	}))
	t.Cleanup(s.api.Close)
	return s
}

func (s *r90provgImageServers) generate(t *testing.T) ([][]byte, error) {
	t.Helper()
	p := NewMaaS(MaaSConfig{
		BaseURL: s.api.URL + "/v1",
		Headers: StaticHeaders(map[string]string{"X-Provider-Key": "test-key"}),
		AuthToken: func(context.Context) (string, error) {
			return "test-token", nil
		},
	})
	model, err := p.ImageModel("test-model")
	if err != nil {
		t.Fatal(err)
	}
	n := 1
	res, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "A small blue square", N: &n})
	if err != nil {
		return nil, err
	}
	return res.Images, nil
}

// TestR90provgMaaSForeignImageDownloadIsValidated ports TS acdccac63d
// (google-vertex-maas-provider-node-download.test.ts): an image URL on a
// foreign origin is fetched through the validated download transport, never
// the MaaS auth transport. The foreign test server is on loopback, which the
// download guard rejects, so it must receive no request and no credential.
func TestR90provgMaaSForeignImageDownloadIsValidated(t *testing.T) {
	s := r90provgStartImageServers(t, func(_, foreign string) string { return foreign + "/image.png" })
	_, err := s.generate(t)
	if err == nil {
		t.Fatal("expected the foreign loopback image URL to be rejected by the download guard")
	}
	if len(s.foreignHit) != 0 {
		t.Fatalf("foreign server got %d requests (headers %v), want 0", len(s.foreignHit), s.foreignHit)
	}
	if len(s.apiHits) != 1 || s.apiHits[0].Get("Authorization") != "Bearer test-token" || s.apiHits[0].Get("X-Provider-Key") != "test-key" {
		t.Fatalf("API request headers = %v, want the token and provider header", s.apiHits)
	}
}

// TestR90provgMaaSRedirectHopIsValidated: an API-origin image URL that
// redirects to a foreign origin has the foreign hop validated (TS: every
// foreign hop, including redirect hops, uses fetchWithValidatedEndpoint).
func TestR90provgMaaSRedirectHopIsValidated(t *testing.T) {
	s := r90provgStartImageServers(t, func(api, _ string) string { return api + "/v1/redirect.png" })
	_, err := s.generate(t)
	if err == nil {
		t.Fatal("expected the foreign loopback redirect hop to be rejected by the download guard")
	}
	if len(s.foreignHit) != 0 {
		t.Fatalf("foreign server got %d requests (headers %v), want 0", len(s.foreignHit), s.foreignHit)
	}
}

// TestR90provgMaaSAPIOriginImageGetsCredentials: an image on the API origin
// (TS credentialedOrigin) is downloaded with the MaaS credentials.
func TestR90provgMaaSAPIOriginImageGetsCredentials(t *testing.T) {
	s := r90provgStartImageServers(t, func(api, _ string) string { return api + "/v1/files/image.png" })
	images, err := s.generate(t)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 || string(images[0]) != string([]byte{4, 5, 6}) {
		t.Fatalf("images = %v, want [[4 5 6]]", images)
	}
	if len(s.apiHits) != 2 {
		t.Fatalf("API requests = %d, want 2", len(s.apiHits))
	}
	if h := s.apiHits[1]; h.Get("Authorization") != "Bearer test-token" {
		t.Fatalf("download Authorization = %q, want the MaaS token", h.Get("Authorization"))
	}
}
