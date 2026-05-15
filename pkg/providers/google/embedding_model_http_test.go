package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestEmbeddingModel_MetadataAndCapabilities(t *testing.T) {
	t.Parallel()

	p := New(Config{APIKey: "test-key"})
	m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)

	if got := m.SpecificationVersion(); got != "v3" {
		t.Fatalf("SpecificationVersion() = %q, want v3", got)
	}
	if got := m.MaxEmbeddingsPerCall(); got != 100 {
		t.Fatalf("MaxEmbeddingsPerCall() = %d, want 100", got)
	}
	if got := m.SupportsParallelCalls(); !got {
		t.Fatal("SupportsParallelCalls() = false, want true")
	}
}

func TestEmbeddingModel_DoEmbed_SuccessAndHeaders(t *testing.T) {
	t.Parallel()

	var gotHeader string
	var gotPath string
	var gotText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Test-Header")
		gotPath = r.URL.Path
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		content := body["content"].(map[string]interface{})
		parts := content["parts"].([]interface{})
		part := parts[0].(map[string]interface{})
		gotText, _ = part["text"].(string)

		w.Header().Set("X-Req-ID", "abc-123")
		_, _ = w.Write([]byte(`{"embedding":{"values":[0.1,0.2,0.3]}}`))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL})
	m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
	res, err := m.DoEmbed(context.Background(), "hello world", &provider.EmbedModelOptions{
		Headers: map[string]string{"X-Test-Header": "ok"},
	})
	if err != nil {
		t.Fatalf("DoEmbed() error = %v", err)
	}
	if gotPath != "/models/"+EmbeddingModelGeminiEmbedding001+":embedContent" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotHeader != "ok" {
		t.Fatalf("X-Test-Header = %q, want ok", gotHeader)
	}
	if gotText != "hello world" {
		t.Fatalf("request text = %q, want hello world", gotText)
	}
	if len(res.Embedding) != 3 {
		t.Fatalf("embedding length = %d, want 3", len(res.Embedding))
	}
	if res.Response.Headers["X-Req-Id"][0] != "abc-123" && res.Response.Headers["X-Req-ID"][0] != "abc-123" {
		t.Fatalf("expected X-Req-ID header to be preserved, got %#v", res.Response.Headers)
	}
}

func TestEmbeddingModel_DoEmbed_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("http failure wrapped as provider error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream-fail", http.StatusBadGateway)
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		_, err := m.DoEmbed(context.Background(), "x", nil)
		if err == nil {
			t.Fatal("expected error")
		}
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("expected provider error, got %T", err)
		}
		if pe.Provider != "google" {
			t.Fatalf("ProviderError.Provider = %q, want google", pe.Provider)
		}
	})

	t.Run("empty embedding response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"embedding":{"values":[]}}`))
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		_, err := m.DoEmbed(context.Background(), "x", nil)
		if err == nil || !strings.Contains(err.Error(), "no embedding data in response") {
			t.Fatalf("expected no embedding data error, got %v", err)
		}
	})
}

func TestEmbeddingModel_DoEmbedMany(t *testing.T) {
	t.Parallel()

	t.Run("empty inputs", func(t *testing.T) {
		p := New(Config{APIKey: "test-key"})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		res, err := m.DoEmbedMany(context.Background(), nil, nil)
		if err != nil {
			t.Fatalf("DoEmbedMany() error = %v", err)
		}
		if len(res.Embeddings) != 0 {
			t.Fatalf("expected empty embeddings, got %d", len(res.Embeddings))
		}
	})

	t.Run("success for multiple inputs", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				_, _ = w.Write([]byte(`{"embedding":{"values":[1,2]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"embedding":{"values":[3,4]}}`))
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		res, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
		if err != nil {
			t.Fatalf("DoEmbedMany() error = %v", err)
		}
		if callCount != 2 {
			t.Fatalf("call count = %d, want 2", callCount)
		}
		if len(res.Embeddings) != 2 || len(res.Responses) != 2 {
			t.Fatalf("unexpected result lens: embeddings=%d responses=%d", len(res.Embeddings), len(res.Responses))
		}
	})

	t.Run("error includes failing index", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				_, _ = w.Write([]byte(`{"embedding":{"values":[1]}}`))
				return
			}
			http.Error(w, "fail-second", http.StatusInternalServerError)
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		_, err := m.DoEmbedMany(context.Background(), []string{"ok", "boom"}, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "failed to embed input 1") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestEmbeddingModel_DoEmbedParts(t *testing.T) {
	t.Parallel()

	t.Run("success with multimodal parts", func(t *testing.T) {
		var sawInlineData bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			content := body["content"].(map[string]interface{})
			parts := content["parts"].([]interface{})
			if len(parts) >= 3 {
				if inline, ok := parts[2].(map[string]interface{})["inlineData"].(map[string]interface{}); ok {
					data, _ := inline["data"].(string)
					sawInlineData = data == base64.StdEncoding.EncodeToString([]byte{0x01, 0x02})
				}
			}
			_, _ = w.Write([]byte(`{"embedding":{"values":[9,8,7]}}`))
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		res, err := m.DoEmbedParts(context.Background(), "primary", []EmbeddingPart{
			TextEmbeddingPart{Text: "extra"},
			ImageEmbeddingPart{MimeType: "image/png", Data: []byte{0x01, 0x02}},
		})
		if err != nil {
			t.Fatalf("DoEmbedParts() error = %v", err)
		}
		if len(res.Embedding) != 3 {
			t.Fatalf("embedding length = %d, want 3", len(res.Embedding))
		}
		if !sawInlineData {
			t.Fatal("expected inlineData base64 payload in request body")
		}
	})

	t.Run("empty response errors", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		_, err := m.DoEmbedParts(context.Background(), "primary", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
