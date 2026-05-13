package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestEmbeddingModelMetadataAndLimits(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewEmbeddingModel(p, "text-embedding-3-large")
	if m.SpecificationVersion() != "v3" || m.Provider() != "openai" || m.ModelID() != "text-embedding-3-large" {
		t.Fatalf("metadata mismatch: spec=%s provider=%s model=%s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}
	if m.MaxEmbeddingsPerCall() != 2048 || !m.SupportsParallelCalls() {
		t.Fatalf("limits mismatch: max=%d parallel=%v", m.MaxEmbeddingsPerCall(), m.SupportsParallelCalls())
	}
}

func TestEmbeddingModelDoEmbedAndDoEmbedMany(t *testing.T) {
	var seenPath string
	var seenAuth string
	var seenHeaders string
	var seenBody map[string]interface{}
	serverURL, closeServer := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		seenHeaders = r.Header.Get("X-Test")
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("X-Request-ID", "req_1")
		w.Header().Set("Content-Type", "application/json")
		if _, ok := seenBody["input"].([]interface{}); ok {
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,2]},{"object":"embedding","index":1,"embedding":[3,4]}],"usage":{"prompt_tokens":7,"total_tokens":9}}`))
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":4}}`))
	}))
	defer closeServer()

	p := New(Config{APIKey: "test-key", BaseURL: serverURL + "/v1"})
	m := NewEmbeddingModel(p, "text-embedding-3-small")

	one, err := m.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{Headers: map[string]string{"X-Test": "yes"}})
	if err != nil {
		t.Fatalf("DoEmbed error = %v", err)
	}
	if seenPath != "/v1/embeddings" || !strings.HasPrefix(seenAuth, "Bearer test-key") || seenHeaders != "yes" {
		t.Fatalf("request mismatch path=%q auth=%q header=%q", seenPath, seenAuth, seenHeaders)
	}
	if seenBody["model"] != "text-embedding-3-small" || seenBody["input"] != "hello" {
		t.Fatalf("request body mismatch: %#v", seenBody)
	}
	if len(one.Embedding) != 2 || one.Usage.InputTokens != 3 || one.Response.Headers["X-Request-Id"][0] != "req_1" {
		t.Fatalf("DoEmbed result mismatch: %#v", one)
	}

	many, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatalf("DoEmbedMany error = %v", err)
	}
	if len(many.Embeddings) != 2 || many.Usage.InputTokens != 7 || many.Responses[0].Headers["X-Request-Id"][0] != "req_1" {
		t.Fatalf("DoEmbedMany result mismatch: %#v", many)
	}
}

func TestEmbeddingModelDoEmbedManyValidationPaths(t *testing.T) {
	emptyServerURL, closeEmpty := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[],"usage":{"prompt_tokens":0,"total_tokens":0}}`))
	}))
	defer closeEmpty()

	p := New(Config{APIKey: "k", BaseURL: emptyServerURL + "/v1"})
	m := NewEmbeddingModel(p, "emb")

	res, err := m.DoEmbedMany(context.Background(), []string{}, nil)
	if err != nil || len(res.Embeddings) != 0 {
		t.Fatalf("empty input should return empty result, got result=%#v err=%v", res, err)
	}

	if _, err := m.DoEmbed(context.Background(), "x", nil); err == nil || !strings.Contains(err.Error(), "no embedding data") {
		t.Fatalf("expected no embedding data error, got %v", err)
	}

	countServerURL, closeCount := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	defer closeCount()

	m2 := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: countServerURL + "/v1"}), "emb")
	if _, err := m2.DoEmbedMany(context.Background(), []string{"a", "b"}, nil); err == nil || !strings.Contains(err.Error(), "expected 2 embeddings, got 1") {
		t.Fatalf("expected count mismatch error, got %v", err)
	}

	indexServerURL, closeIndex := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":1,"embedding":[1]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	defer closeIndex()

	m3 := NewEmbeddingModel(New(Config{APIKey: "k", BaseURL: indexServerURL + "/v1"}), "emb")
	if _, err := m3.DoEmbedMany(context.Background(), []string{"a"}, nil); err == nil || !strings.Contains(err.Error(), "embedding index mismatch") {
		t.Fatalf("expected index mismatch error, got %v", err)
	}
}

func TestEmbeddingModelHandleErrorAndOptsHeaders(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewEmbeddingModel(p, "emb")
	baseErr := errors.New("boom")
	err := m.handleError(baseErr)
	if !providererrors.IsProviderError(err) {
		t.Fatalf("expected provider error, got %T", err)
	}
	if h := optsHeaders(nil); h != nil {
		t.Fatalf("optsHeaders(nil) = %#v, want nil", h)
	}
}

func newOpenAIIPv4TestServer(t *testing.T, handler http.Handler) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen tcp4 error = %v", err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(listener) }()
	baseURL := "http://127.0.0.1:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	closeFn := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	return baseURL, closeFn
}
