package googlevertex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMaaSProvider_WrapperMethodsAndUnsupported(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		case "/embeddings":
			_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2],"index":0}],"model":"text-embedding-3-small","usage":{"prompt_tokens":2,"total_tokens":2}}`))
		case "/images/generations":
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"aGVsbG8="}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	p := NewMaaS(MaaSConfig{
		BaseURL: server.URL,
		AuthToken: func(context.Context) (string, error) {
			return "token", nil
		},
	})

	if _, err := p.ChatModel("test-model"); err != nil {
		t.Fatalf("ChatModel() error = %v", err)
	}
	if _, err := p.CompletionModel("test-model"); err != nil {
		t.Fatalf("CompletionModel() error = %v", err)
	}
	if _, err := p.EmbeddingModel("text-embedding-3-small"); err != nil {
		t.Fatalf("EmbeddingModel() error = %v", err)
	}
	if _, err := p.TextEmbeddingModel("text-embedding-3-small"); err != nil {
		t.Fatalf("TextEmbeddingModel() error = %v", err)
	}
	if _, err := p.ImageModel("gpt-image-1"); err != nil {
		t.Fatalf("ImageModel() error = %v", err)
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
