package bfl

import (
	"context"
	"encoding/json"
	"io"
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
	if m.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if m.Provider() != "bfl" {
		t.Fatalf("Provider() = %q", m.Provider())
	}
}

func TestBFLImageModel_DoGenerateSuccess(t *testing.T) {
	t.Parallel()

	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/flux-pro":
			if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_, _ = w.Write([]byte(`{"id":"req-1","polling_url":"http://` + r.Host + `/get_result","cost":0.12,"input_mp":1.5,"output_mp":1.0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/get_result":
			_, _ = w.Write([]byte(`{"id":"req-1","status":"Ready","result":{"sample":"data:image/png;base64,iVBORw==","seed":123,"start_time":1,"end_time":2,"duration":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

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
	if res.Response == nil || res.Response.ModelID != "flux-pro" {
		t.Fatalf("response metadata = %#v", res.Response)
	}
	if res.Response.Timestamp.IsZero() {
		t.Fatalf("response timestamp must be set like TS response.timestamp")
	}
	if capturedBody["aspect_ratio"] != "1:1" {
		t.Fatalf("aspect_ratio = %#v, want 1:1", capturedBody["aspect_ratio"])
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Feature != "size" {
		t.Fatalf("warnings = %#v", res.Warnings)
	}
	metadata, ok := res.ProviderMetadata["blackForestLabs"].(map[string]interface{})
	if !ok {
		t.Fatalf("provider metadata = %#v", res.ProviderMetadata)
	}
	images := metadata["images"].([]map[string]interface{})
	if images[0]["cost"] != 0.12 || images[0]["inputMegapixels"] != 1.5 || images[0]["seed"] != 123.0 {
		t.Fatalf("metadata image = %#v", images[0])
	}
}

func TestBFLImageModel_HeadersForTrustedAndForeignURLs(t *testing.T) {
	t.Parallel()

	var submitHeaders http.Header
	var pollHeaders http.Header
	var imageHeaders http.Header
	var foreignHeaders http.Header
	useForeign := false
	oldDefaultTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "delivery-us1.bfl.ai":
			imageHeaders = r.Header.Clone()
		case "foreign.example":
			foreignHeaders = r.Header.Clone()
		default:
			return oldDefaultTransport.RoundTrip(r)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader("image")),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = oldDefaultTransport })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			submitHeaders = r.Header.Clone()
			_, _ = w.Write([]byte(`{"id":"req-headers","polling_url":"http://` + r.Host + `/poll"}`))
		case "/poll":
			pollHeaders = r.Header.Clone()
			sample := "https://delivery-us1.bfl.ai/image.png"
			if useForeign {
				sample = "http://foreign.example/image.png"
			}
			_, _ = w.Write([]byte(`{"status":"Ready","result":{"sample":"` + sample + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{
		APIKey:  "k",
		BaseURL: server.URL,
		Headers: map[string]string{
			"Custom-Provider-Header": "provider",
		},
	})
	m := NewImageModel(p, "flux-pro")
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"pollIntervalMillis": 1},
		},
		Headers: map[string]string{"Custom-Request-Header": "request"},
	})
	if err != nil {
		t.Fatalf("DoGenerate trusted: %v", err)
	}
	if submitHeaders.Get("Custom-Provider-Header") != "provider" || submitHeaders.Get("Custom-Request-Header") != "request" {
		t.Fatalf("submit headers = %#v", submitHeaders)
	}
	if pollHeaders.Get("Custom-Provider-Header") != "provider" || pollHeaders.Get("Custom-Request-Header") != "request" || pollHeaders.Get("Content-Type") != "" {
		t.Fatalf("poll headers = %#v", pollHeaders)
	}
	if imageHeaders.Get("Custom-Provider-Header") != "provider" || imageHeaders.Get("Custom-Request-Header") != "request" {
		t.Fatalf("image headers = %#v", imageHeaders)
	}

	useForeign = true
	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a mountain",
		ProviderOptions: map[string]interface{}{
			"blackForestLabs": map[string]interface{}{"pollIntervalMillis": 1},
		},
		Headers: map[string]string{"Custom-Request-Header": "request"},
	})
	if err != nil {
		t.Fatalf("DoGenerate foreign: %v", err)
	}
	if foreignHeaders.Get("X-Key") != "" || foreignHeaders.Get("Custom-Provider-Header") != "" || foreignHeaders.Get("Custom-Request-Header") != "" {
		t.Fatalf("foreign headers = %#v", foreignHeaders)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
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
				_, _ = w.Write([]byte(`{"id":"req-2","polling_url":"http://` + r.Host + `/get_result"}`))
			case "/get_result":
				_, _ = w.Write([]byte(`{"id":"req-2","status":"Failed","result":{}}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		p := New(Config{APIKey: "k", BaseURL: server.URL})
		m := NewImageModel(p, "flux-pro")
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), "Black Forest Labs generation failed.") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestBFLImageModel_SubmitResponseRequiresPollingURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"req-missing"}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "flux-pro")
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "missing polling_url") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBFLImageModel_UsesPollingURLAndStateAlias(t *testing.T) {
	t.Parallel()

	var sawProviderPollURL bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flux-pro":
			_, _ = w.Write([]byte(`{"id":"req-3","polling_url":"http://` + r.Host + `/custom_poll"}`))
		case "/custom_poll":
			sawProviderPollURL = true
			if r.URL.Query().Get("id") != "req-3" {
				t.Fatalf("poll id = %q", r.URL.Query().Get("id"))
			}
			if got := r.Header.Get("X-Key"); got != "k" {
				t.Fatalf("X-Key = %q", got)
			}
			_, _ = w.Write([]byte(`{"state":"Ready","result":{"sample":"data:image/png;base64,iVBORw=="}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "flux-pro")
	createResp := bflCreateResponse{ID: "req-3", PollingURL: server.URL + "/custom_poll"}
	result, err := m.pollResult(context.Background(), createResp, bflPollConfig{}, map[string]string{"Custom": "value"})
	if err != nil {
		t.Fatalf("pollResult: %v", err)
	}
	if !sawProviderPollURL || result.Result.Sample == "" {
		t.Fatalf("pollURL used=%v result=%#v", sawProviderPollURL, result)
	}
}

func TestBFLImageModel_PollResultContextCancelled(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "k", BaseURL: "https://example.test"})
	m := NewImageModel(p, "flux-pro")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.pollResult(ctx, bflCreateResponse{ID: "req"}, bflPollConfig{}, nil)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("unexpected error: %v", err)
	}
}
