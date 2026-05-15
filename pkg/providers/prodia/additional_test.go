package prodia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestProdiaProvider_ResolvedDefaultsAndUnsupported(t *testing.T) {
	t.Parallel()

	_ = os.Setenv("PRODIA_TOKEN", "env-token")
	t.Cleanup(func() { _ = os.Unsetenv("PRODIA_TOKEN") })

	p := New(Config{})
	if p.Name() != "prodia" {
		t.Fatalf("Name() = %q, want prodia", p.Name())
	}
	if p.effectiveBaseURL() != "https://inference.prodia.com/v2" {
		t.Fatalf("effectiveBaseURL() mismatch: %q", p.effectiveBaseURL())
	}
	if p.effectiveAPIKey() != "env-token" {
		t.Fatalf("effectiveAPIKey() = %q", p.effectiveAPIKey())
	}

	if _, err := p.EmbeddingModel("x"); err == nil {
		t.Fatal("EmbeddingModel expected unsupported error")
	}
	if _, err := p.SpeechModel("x"); err == nil {
		t.Fatal("SpeechModel expected unsupported error")
	}
	if _, err := p.TranscriptionModel("x"); err == nil {
		t.Fatal("TranscriptionModel expected unsupported error")
	}
	if _, err := p.RerankingModel("x"); err == nil {
		t.Fatal("RerankingModel expected unsupported error")
	}
}

func TestProdiaProvider_ModelRoutingAndMetadata(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "k", BaseURL: "https://example.test"})

	im, err := p.ImageModel("")
	if err != nil || im == nil {
		t.Fatalf("ImageModel() error = %v", err)
	}
	if im.Provider() != "prodia.image" {
		t.Fatalf("image provider = %q", im.Provider())
	}
	if im.SpecificationVersion() != "v3" {
		t.Fatalf("image spec version = %q", im.SpecificationVersion())
	}

	if _, err := p.LanguageModel("bad-model"); err == nil {
		t.Fatal("LanguageModel should reject unsupported model prefix")
	}
	if _, err := p.VideoModel("bad-model"); err == nil {
		t.Fatal("VideoModel should reject unsupported model prefix")
	}
}

func TestProdiaLanguageModel_DoStreamAndStreamHelpers(t *testing.T) {
	t.Parallel()

	body, contentType := buildTestMultipartBody(`{"id":"job-1","config":{"seed":42},"price":{"dollars":0.01}}`, []byte("hello"), "text/plain")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/job") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m := NewLanguageModel(p, LanguageModelNanoBananaImgToImgV2)

	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()

	var sawStart, sawText, sawFinish bool
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		switch chunk.Type {
		case provider.ChunkTypeStreamStart:
			sawStart = true
		case provider.ChunkTypeText:
			sawText = chunk.Text == "hello"
		case provider.ChunkTypeFinish:
			sawFinish = true
		}
	}
	if !sawStart || !sawText || !sawFinish {
		t.Fatalf("unexpected stream chunks: start=%v text=%v finish=%v", sawStart, sawText, sawFinish)
	}

	ps := &prodiaTextStream{chunks: []*provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "x"}}}
	if _, err := ps.Next(); err != nil {
		t.Fatalf("prodiaTextStream.Next() unexpected error: %v", err)
	}
	if ps.Err() != nil {
		t.Fatalf("prodiaTextStream.Err() = %v, want nil", ps.Err())
	}
	_ = ps.Close()
	if _, err := ps.Next(); err == nil {
		t.Fatal("prodiaTextStream.Next() after close expected EOF")
	}
}

func TestProdiaAPIHelpers_PostMultipartAndMediaTypeExt(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/form-data; boundary=foo")
		_, _ = w.Write([]byte("--foo\r\nContent-Disposition: form-data; name=\"job\"\r\nContent-Type: application/json\r\n\r\n{\"id\":\"ok\"}\r\n--foo--\r\n"))
	}))
	defer srv.Close()

	buf, ct, err := buildMultipartJobRequest(map[string]interface{}{"x": 1}, []byte("img"), "image/png")
	if err != nil {
		t.Fatalf("buildMultipartJobRequest() error = %v", err)
	}
	respBody, headers, err := postMultipartToProdia(context.Background(), srv.URL, "k", buf, ct, "multipart/form-data")
	if err != nil {
		t.Fatalf("postMultipartToProdia() error = %v", err)
	}
	if len(respBody) == 0 || headers.Get("Content-Type") == "" {
		t.Fatalf("unexpected postMultipartToProdia response: body=%q headers=%v", string(respBody), headers)
	}

	if mediaTypeToExt("video/mp4") != ".mp4" || mediaTypeToExt("unknown") != "" {
		t.Fatalf("mediaTypeToExt mapping mismatch")
	}
}
