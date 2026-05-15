package bfl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestBFLImageModel_MetadataAndClient(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "k"})
	if p.Client() == nil {
		t.Fatal("Client() returned nil")
	}
	m := NewImageModel(p, "flux-pro")
	if m.SpecificationVersion() != "v3" {
		t.Fatalf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if m.Provider() != "bfl" {
		t.Fatalf("Provider() = %q", m.Provider())
	}
}

func TestBFLImageModel_DoGenerateSuccess(t *testing.T) {
	t.Parallel()

	image := []byte{0x89, 0x50, 0x4E, 0x47}
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/flux-pro":
			_, _ = w.Write([]byte(`{"id":"req-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/get_result":
			_, _ = w.Write([]byte(`{"id":"req-1","status":"Ready","result":{"sample":"` + serverURL + `/image.png"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/image.png":
			_, _ = w.Write(image)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "flux-pro")
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		Size:   "1024x1024",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(res.Image) == 0 {
		t.Fatal("image bytes must not be empty")
	}
	if res.URL == "" {
		t.Fatal("result URL must be set")
	}
}

func TestBFLImageModel_DoGenerateErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("create call non-200", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad request", http.StatusBadRequest)
		}))
		defer server.Close()

		p := New(Config{APIKey: "k", BaseURL: server.URL})
		m := NewImageModel(p, "flux-pro")
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), "status 400") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("poll returns error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/flux-pro":
				_, _ = w.Write([]byte(`{"id":"req-2"}`))
			case "/get_result":
				_, _ = w.Write([]byte(`{"id":"req-2","status":"Error","result":{}}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		p := New(Config{APIKey: "k", BaseURL: server.URL})
		m := NewImageModel(p, "flux-pro")
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), "request Error") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestBFLImageModel_PollResultContextCancelled(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "k", BaseURL: "https://example.test"})
	m := NewImageModel(p, "flux-pro")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.pollResult(ctx, "req")
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("unexpected error: %v", err)
	}
}
