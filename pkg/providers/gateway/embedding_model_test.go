package gateway

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
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

func TestGatewayEmbeddingModelMetadataAndHeaders(t *testing.T) {
	m := NewEmbeddingModel(&Provider{}, "openai/text-embedding-3-small")
	if m.SpecificationVersion() != "v4" || m.Provider() != "gateway" || m.ModelID() != "openai/text-embedding-3-small" {
		t.Fatalf("metadata mismatch")
	}
	if m.MaxEmbeddingsPerCall() != 2048 || !m.SupportsParallelCalls() {
		t.Fatalf("limits mismatch")
	}
	headers := m.getModelConfigHeaders()
	if headers["ai-embedding-model-specification-version"] != "4" || headers["ai-model-id"] != "openai/text-embedding-3-small" {
		t.Fatalf("config headers mismatch: %#v", headers)
	}
}

func TestGatewayEmbeddingModelDoEmbedAndMany(t *testing.T) {
	var seenPath string
	var seenHeader string
	var seenBody map[string]interface{}
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenHeader = r.Header.Get("X-Test")
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("X-Req", "r1")
		w.Header().Set("Content-Type", "application/json")
		values := seenBody["values"].([]interface{})
		if len(values) == 1 {
			_, _ = w.Write([]byte(`{"embeddings":[[1,2,3]],"usage":{"tokens":1},"warnings":[{"type":"deprecated","setting":"old","message":"use new"}],"providerMetadata":{"gateway":{"id":"one"}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"embeddings":[[1,2],[3,4]],"usage":{"tokens":2},"warnings":[{"type":"other","message":"note"}],"providerMetadata":{"gateway":{"id":"many"}}}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	m := NewEmbeddingModel(p, "openai/text-embedding-3-small")

	one, err := m.DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{
		Headers: map[string]string{"X-Test": "yes"},
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"dimensions": 128},
		},
	})
	if err != nil {
		t.Fatalf("DoEmbed error = %v", err)
	}
	if seenPath != "/v4/ai/embedding-model" || seenHeader != "yes" || seenBody["values"].([]interface{})[0] != "hello" {
		t.Fatalf("request mismatch path=%q header=%q body=%#v", seenPath, seenHeader, seenBody)
	}
	if seenBody["providerOptions"].(map[string]interface{})["openai"].(map[string]interface{})["dimensions"] != float64(128) {
		t.Fatalf("providerOptions mismatch: %#v", seenBody)
	}
	if len(one.Embedding) != 3 || one.Response.Headers["X-Req"] != "r1" || one.Response.Body == nil || one.Usage.Tokens != 1 || one.ProviderMetadata["gateway"] == nil {
		t.Fatalf("result mismatch: %#v", one)
	}
	if len(one.Warnings) != 1 || one.Warnings[0].Type != "deprecated" || one.Warnings[0].Setting != "old" || one.Warnings[0].Message != "use new" {
		t.Fatalf("warnings = %#v", one.Warnings)
	}

	many, err := m.DoEmbedMany(context.Background(), []string{"a", "b"}, nil)
	if err != nil {
		t.Fatalf("DoEmbedMany error = %v", err)
	}
	if len(many.Embeddings) != 2 || many.Responses[0].Headers["X-Req"] != "r1" || many.Responses[0].Body == nil || many.Usage.Tokens != 2 || many.ProviderMetadata["gateway"] == nil {
		t.Fatalf("many result mismatch: %#v", many)
	}
	if len(many.Warnings) != 1 || many.Warnings[0].Type != "other" || many.Warnings[0].Message != "note" {
		t.Fatalf("many warnings = %#v", many.Warnings)
	}
}

func TestGatewayEmbeddingModelPreservesEmptyProviderOptions(t *testing.T) {
	var seenBody map[string]interface{}
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embeddings":[[1]],"usage":{"tokens":1}}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	_, err = NewEmbeddingModel(p, "openai/text-embedding-3-small").DoEmbed(context.Background(), "hello", &provider.EmbedModelOptions{
		ProviderOptions: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("DoEmbed error = %v", err)
	}
	if providerOptions, ok := seenBody["providerOptions"].(map[string]interface{}); !ok || len(providerOptions) != 0 {
		t.Fatalf("empty providerOptions should be preserved when provided: %#v", seenBody)
	}
}

func TestGatewayEmbeddingModelHandleError(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "https://example.com/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	m := NewEmbeddingModel(p, "m")
	wrapped := m.handleError(errors.New("boom"))
	var responseErr *gatewayerrors.GatewayResponseError
	if !errors.As(wrapped, &responseErr) {
		t.Fatalf("expected gateway response error, got %T", wrapped)
	}
	if responseErr.GetStatusCode() != http.StatusInternalServerError || !responseErr.IsRetryable() {
		t.Fatalf("unexpected gateway response error metadata: status=%d retryable=%v", responseErr.GetStatusCode(), responseErr.IsRetryable())
	}
	if !strings.Contains(responseErr.Error(), "Gateway request failed: boom") {
		t.Fatalf("unexpected error message: %q", responseErr.Error())
	}
}

func newGatewayIPv4TestServer(t *testing.T, handler http.Handler) (string, func()) {
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
