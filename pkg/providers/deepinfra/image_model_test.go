package deepinfra

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ported (behaviorally) from ai/packages/deepinfra/src/deepinfra-image-model.test.ts

func TestDeepInfraImageModel_Generate(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":["data:image/png;base64,` + base64.StdEncoding.EncodeToString([]byte("fake-image")) + `"]}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL + "/openai"})
	model, err := p.ImageModel("stabilityai/sd3.5")
	if err != nil {
		t.Fatalf("ImageModel() error = %v", err)
	}
	n := 1
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat",
		N:      &n,
		Size:   "512x512",
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gotPath != "/inference/stabilityai/sd3.5" {
		t.Fatalf("path = %q, want /inference/stabilityai/sd3.5", gotPath)
	}
	if gotBody["width"] != "512" || gotBody["height"] != "512" {
		t.Fatalf("width/height = %#v/%#v", gotBody["width"], gotBody["height"])
	}
	if len(result.Images) != 1 || string(result.Images[0]) != "fake-image" {
		t.Fatalf("Images = %#v", result.Images)
	}
}

func TestDeepInfraImageModel_ProviderOptionsPassthrough(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":[]}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL + "/openai"})
	model, err := p.ImageModel("black-forest-labs/FLUX-1-dev")
	if err != nil {
		t.Fatalf("ImageModel() error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "a cat",
		ProviderOptions: map[string]interface{}{
			"deepinfra": map[string]interface{}{
				"negative_prompt":     "blurry",
				"num_inference_steps": float64(30),
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gotBody["negative_prompt"] != "blurry" {
		t.Fatalf("negative_prompt = %#v", gotBody["negative_prompt"])
	}
	if gotBody["num_inference_steps"] != float64(30) {
		t.Fatalf("num_inference_steps = %#v", gotBody["num_inference_steps"])
	}
}

func TestDeepInfraImageModel_GenerateError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":{"error":"invalid model"}}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL + "/openai"})
	model, err := p.ImageModel("bad-model")
	if err != nil {
		t.Fatalf("ImageModel() error = %v", err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "a cat"})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestDeepInfraImageModel_Edit(t *testing.T) {
	var gotPath string
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte("edited")) + `"}]}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL + "/openai"})
	model, err := p.ImageModel("black-forest-labs/FLUX.1-Kontext-dev")
	if err != nil {
		t.Fatalf("ImageModel() error = %v", err)
	}
	result, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt: "make it blue",
		Files: []provider.ImageFile{
			{Type: "file", Data: []byte("source-image"), MediaType: "image/png"},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gotPath != "/openai/images/edits" {
		t.Fatalf("path = %q, want /openai/images/edits", gotPath)
	}
	if gotContentType == "" {
		t.Fatal("expected a multipart Content-Type header")
	}
	if len(result.Images) != 1 || string(result.Images[0]) != "edited" {
		t.Fatalf("Images = %#v", result.Images)
	}
}

// TestDeepInfraImageModel_EditDoesNotLeakPipeGoroutineOnDialFailure checks a
// bug report's "Unverified" finding: buildEditMultipartBody writes the
// multipart body on a goroutine via io.Pipe, piped straight into the
// request body. If the underlying transport ever fails to read/close that
// body without draining it, the writer goroutine could block forever on a
// pw.Write() call that never gets a matching pr.Read().
//
// In practice this does not leak: net/http.Transport.RoundTrip's documented
// contract is that it always closes the request body, including on dial
// failure, even if it never reads from it. Closing the io.PipeReader side
// unblocks any pending/future io.PipeWriter.Write() with io.ErrClosedPipe,
// which the writer goroutine's error path turns into pw.CloseWithError and
// returns from. This mirrors the equivalent, already-confirmed-safe pattern
// in openai/image_model.go's doEdit/buildEditMultipartBody (same io.Pipe
// shape, same net/http.Transport contract).
//
// This test confirms that behavior empirically for deepinfra specifically:
// repeated edit calls against an address that fails to dial (connection
// refused) must not leave any writer goroutines behind.
func TestDeepInfraImageModel_EditDoesNotLeakPipeGoroutineOnDialFailure(t *testing.T) {
	// A server that is immediately closed yields a deterministic, fast
	// "connection refused" dial failure without needing a real network
	// outage.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachable := srv.URL
	srv.Close()

	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	p := New(Config{APIKey: "k", BaseURL: unreachable + "/openai"})
	model, err := p.ImageModel("black-forest-labs/FLUX.1-Kontext-dev")
	if err != nil {
		t.Fatalf("ImageModel() error = %v", err)
	}

	const n = 10
	for i := 0; i < n; i++ {
		_, err := model.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Prompt: "make it blue",
			Files: []provider.ImageFile{
				{Type: "file", Data: []byte("source-image"), MediaType: "image/png"},
			},
		})
		if err == nil {
			t.Fatal("expected a dial error against a closed server")
		}
	}

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	if after-baseline >= n/2 {
		t.Fatalf("goroutine leak: baseline=%d after=%d (leaked ~%d of %d buildEditMultipartBody calls)", baseline, after, after-baseline, n)
	}
}

func TestDeriveDeepInfraImageBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://api.deepinfra.com/v1/openai":  "https://api.deepinfra.com/v1/inference",
		"https://api.deepinfra.com/v1/openai/": "https://api.deepinfra.com/v1/inference",
		"https://example.invalid":              "https://example.invalid/inference",
	}
	for in, want := range cases {
		if got := deriveDeepInfraImageBaseURL(in); got != want {
			t.Errorf("deriveDeepInfraImageBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}
