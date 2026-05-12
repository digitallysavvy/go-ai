package together

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestImageModelBuildRequestBodyAndConvertResponse(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewImageModel(p, "img-model")
	if m.SpecificationVersion() != "v3" || m.Provider() != "together" || m.ModelID() != "img-model" {
		t.Fatalf("unexpected model metadata")
	}

	n := 2
	body := m.buildRequestBody(&provider.ImageGenerateOptions{
		Prompt: "a mountain",
		N:      &n,
		Size:   "1024x768",
	})
	if body["model"] != "img-model" || body["prompt"] != "a mountain" || body["n"] != 2 {
		t.Fatalf("unexpected body: %+v", body)
	}
	if body["width"] != 1024 || body["height"] != 768 {
		t.Fatalf("expected parsed dimensions, got %+v", body)
	}

	_, err := m.convertResponse([]byte(`{"data":[]}`))
	if err == nil {
		t.Fatal("expected error when no images returned")
	}

	res, err := m.convertResponse([]byte(`{"data":[{"url":"https://example.com/a.png"}]}`))
	if err != nil || res.URL == "" || res.MimeType != "image/png" {
		t.Fatalf("url response conversion failed: res=%+v err=%v", res, err)
	}
}

func TestImageModelDoGenerate(t *testing.T) {
	var status int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(status)
		if status == 200 {
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"Zm9v"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "k", BaseURL: server.URL})
	m := NewImageModel(p, "img-model")

	status = 200
	okRes, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if err != nil {
		t.Fatalf("DoGenerate success path error = %v", err)
	}
	if string(okRes.Image) != "Zm9v" || okRes.MimeType != "image/png" {
		t.Fatalf("unexpected success response: %+v", okRes)
	}

	status = 500
	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if err == nil {
		t.Fatal("expected non-200 error")
	}
}
