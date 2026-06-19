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

	if got := m.SpecificationVersion(); got != "v4" {
		t.Fatalf("SpecificationVersion() = %q, want v4", got)
	}
	if got := m.MaxEmbeddingsPerCall(); got != 2048 {
		t.Fatalf("MaxEmbeddingsPerCall() = %d, want 2048", got)
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
	if res.Response.Headers["X-Req-Id"] != "abc-123" && res.Response.Headers["X-Req-ID"] != "abc-123" {
		t.Fatalf("expected X-Req-ID header to be preserved, got %#v", res.Response.Headers)
	}
	if string(res.Response.Body.(json.RawMessage)) == "" {
		t.Fatal("expected raw response body")
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
		var gotPath string
		var requestCount int
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			gotPath = r.URL.Path
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			requests := body["requests"].([]interface{})
			requestCount = len(requests)
			_, _ = w.Write([]byte(`{"embeddings":[{"values":[1,2]},{"values":[3,4]}]}`))
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		res, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
		if err != nil {
			t.Fatalf("DoEmbedMany() error = %v", err)
		}
		if callCount != 1 {
			t.Fatalf("call count = %d, want 1", callCount)
		}
		if gotPath != "/models/"+EmbeddingModelGeminiEmbedding001+":batchEmbedContents" {
			t.Fatalf("path = %q", gotPath)
		}
		if requestCount != 2 {
			t.Fatalf("batch request count = %d, want 2", requestCount)
		}
		if len(res.Embeddings) != 2 || len(res.Responses) != 1 {
			t.Fatalf("unexpected result lens: embeddings=%d responses=%d", len(res.Embeddings), len(res.Responses))
		}
	})

	t.Run("batch error is wrapped", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "fail-second", http.StatusInternalServerError)
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		_, err := m.DoEmbedMany(context.Background(), []string{"ok", "boom"}, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "fail-second") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("too many values returns provider error", func(t *testing.T) {
		p := New(Config{APIKey: "test-key"})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		inputs := make([]string, m.MaxEmbeddingsPerCall()+1)
		_, err := m.DoEmbedMany(context.Background(), inputs, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("expected ProviderError, got %T", err)
		}
		if pe.ErrorCode != "too_many_embedding_values_for_call" {
			t.Fatalf("error code = %q", pe.ErrorCode)
		}
	})
}

func TestEmbeddingModel_ProviderOptionsContentParity(t *testing.T) {
	t.Parallel()

	t.Run("single embeds fileData content and model options", func(t *testing.T) {
		var captured map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/models/"+EmbeddingModelGeminiEmbedding001+":embedContent" {
				t.Fatalf("path = %q", r.URL.Path)
			}
			_ = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write([]byte(`{"embedding":{"values":[0.4,0.5]}}`))
		}))
		defer server.Close()

		dims := 128
		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		_, err := m.DoEmbed(context.Background(), "caption", &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{"google": GoogleEmbeddingProviderOptions{
				OutputDimensionality: &dims,
				TaskType:             "RETRIEVAL_DOCUMENT",
				Content: [][]EmbeddingPart{{
					FileDataEmbeddingPart{MimeType: "application/pdf", FileURI: "files/sample"},
				}},
			}},
		})
		if err != nil {
			t.Fatalf("DoEmbed() error = %v", err)
		}
		if captured["outputDimensionality"] != float64(128) {
			t.Fatalf("outputDimensionality = %#v", captured["outputDimensionality"])
		}
		if captured["taskType"] != "RETRIEVAL_DOCUMENT" {
			t.Fatalf("taskType = %#v", captured["taskType"])
		}
		parts := captured["content"].(map[string]interface{})["parts"].([]interface{})
		if len(parts) != 2 {
			t.Fatalf("parts len = %d, want 2", len(parts))
		}
		fileData := parts[1].(map[string]interface{})["fileData"].(map[string]interface{})
		if fileData["fileUri"] != "files/sample" || fileData["mimeType"] != "application/pdf" {
			t.Fatalf("fileData = %#v", fileData)
		}
	})

	t.Run("batch embeds per-value fileData content", func(t *testing.T) {
		var captured map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/models/"+EmbeddingModelGeminiEmbedding001+":batchEmbedContents" {
				t.Fatalf("path = %q", r.URL.Path)
			}
			_ = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write([]byte(`{"embeddings":[{"values":[1]},{"values":[2]}]}`))
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		_, err := m.DoEmbedMany(context.Background(), []string{"", "query"}, &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
				"content": []interface{}{
					[]interface{}{map[string]interface{}{"fileData": map[string]interface{}{"mimeType": "application/pdf", "fileUri": "files/a"}}},
					nil,
				},
			}},
		})
		if err != nil {
			t.Fatalf("DoEmbedMany() error = %v", err)
		}
		requests := captured["requests"].([]interface{})
		firstParts := requests[0].(map[string]interface{})["content"].(map[string]interface{})["parts"].([]interface{})
		if len(firstParts) != 1 {
			t.Fatalf("first parts len = %d, want file only", len(firstParts))
		}
		secondParts := requests[1].(map[string]interface{})["content"].(map[string]interface{})["parts"].([]interface{})
		if secondParts[0].(map[string]interface{})["text"] != "query" {
			t.Fatalf("second text part = %#v", secondParts[0])
		}
	})

	t.Run("content length must match values", func(t *testing.T) {
		p := New(Config{APIKey: "test-key", BaseURL: "http://127.0.0.1:1"})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		_, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{"google": GoogleEmbeddingProviderOptions{Content: [][]EmbeddingPart{{TextEmbeddingPart{Text: "x"}}}}},
		})
		if err == nil || !strings.Contains(err.Error(), "must match the number of values") {
			t.Fatalf("expected content length error, got %v", err)
		}
	})

	t.Run("map options preserve TS number and inlineData string", func(t *testing.T) {
		var captured map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write([]byte(`{"embedding":{"values":[0.1]}}`))
		}))
		defer server.Close()

		p := New(Config{APIKey: "test-key", BaseURL: server.URL})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)
		_, err := m.DoEmbed(context.Background(), "caption", &provider.EmbedModelOptions{
			ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
				"outputDimensionality": 127.5,
				"content": []interface{}{[]interface{}{
					map[string]interface{}{"inlineData": map[string]interface{}{"mimeType": "image/png", "data": "not-base64-but-schema-valid"}},
				}},
			}},
		})
		if err != nil {
			t.Fatalf("DoEmbed() error = %v", err)
		}
		if captured["outputDimensionality"] != 127.5 {
			t.Fatalf("outputDimensionality = %#v", captured["outputDimensionality"])
		}
		parts := captured["content"].(map[string]interface{})["parts"].([]interface{})
		inline := parts[1].(map[string]interface{})["inlineData"].(map[string]interface{})
		if inline["data"] != "not-base64-but-schema-valid" {
			t.Fatalf("inline data = %#v", inline["data"])
		}
	})
}

func TestEmbeddingModel_DoEmbedParts(t *testing.T) {
	t.Parallel()

	t.Run("success with multimodal parts", func(t *testing.T) {
		var sawInlineData bool
		var sawFileData bool
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
			if len(parts) >= 4 {
				if fileData, ok := parts[3].(map[string]interface{})["fileData"].(map[string]interface{}); ok {
					mimeType, _ := fileData["mimeType"].(string)
					fileURI, _ := fileData["fileUri"].(string)
					sawFileData = mimeType == "application/pdf" && fileURI == "files/abc123"
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
			FileDataEmbeddingPart{MimeType: "application/pdf", FileURI: "files/abc123"},
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
		if !sawFileData {
			t.Fatal("expected fileData payload in request body")
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

	t.Run("validates image and fileData parts", func(t *testing.T) {
		p := New(Config{APIKey: "test-key", BaseURL: "http://127.0.0.1:1"})
		m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding001)

		_, err := m.DoEmbedParts(context.Background(), "primary", []EmbeddingPart{
			ImageEmbeddingPart{MimeType: "", Data: []byte{0x01}},
		})
		if err == nil || !strings.Contains(err.Error(), "image embedding part mime type cannot be empty") {
			t.Fatalf("expected image mime type validation error, got %v", err)
		}

		_, err = m.DoEmbedParts(context.Background(), "primary", []EmbeddingPart{
			FileDataEmbeddingPart{MimeType: "application/pdf", FileURI: ""},
		})
		if err == nil || !strings.Contains(err.Error(), "fileData embedding part file URI cannot be empty") {
			t.Fatalf("expected fileData URI validation error, got %v", err)
		}
	})
}
