package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

func TestGatewayImageModelMetadataAndHeaders(t *testing.T) {
	m := NewImageModel(&Provider{}, "openai/gpt-image-1")
	if m.SpecificationVersion() != "v3" || m.Provider() != "gateway" || m.ModelID() != "openai/gpt-image-1" {
		t.Fatalf("metadata mismatch")
	}
	headers := m.getModelConfigHeaders()
	if headers["ai-image-model-specification-version"] != "3" || headers["ai-image-model-id"] != "openai/gpt-image-1" {
		t.Fatalf("headers mismatch: %#v", headers)
	}
}

func TestGatewayImageModelDoGenerate(t *testing.T) {
	var seenPath string
	var seenBody map[string]interface{}
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"url":"https://example.com/image.png","mimeType":"image/png"}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	m := NewImageModel(p, "openai/gpt-image-1")
	n := 2
	_, err = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:  "draw cat",
		N:       &n,
		Size:    "1024x1024",
		Quality: "high",
		Style:   "vivid",
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenPath != "/v4/ai/image-model" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenBody["prompt"] != "draw cat" || seenBody["n"] != float64(2) || seenBody["size"] != "1024x1024" || seenBody["quality"] != "high" || seenBody["style"] != "vivid" {
		t.Fatalf("body mismatch: %#v", seenBody)
	}
}

func TestGatewayImageModelHandleError(t *testing.T) {
	p, err := New(Config{APIKey: "k", BaseURL: "https://example.com/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	m := NewImageModel(p, "img")

	timeoutErr := m.handleError(gatewayTimeoutTestError{})
	var gte *gatewayerrors.GatewayTimeoutError
	if !errors.As(timeoutErr, &gte) {
		t.Fatalf("expected gateway timeout error, got %T (%v)", timeoutErr, timeoutErr)
	}

	providerErr := providererrors.NewProviderError("gateway", 0, "", "x", nil)
	if got := m.handleError(providerErr); got != providerErr {
		t.Fatalf("existing provider error should pass through, got %v", got)
	}

	wrapped := m.handleError(errors.New("boom"))
	var responseErr *gatewayerrors.GatewayResponseError
	if !errors.As(wrapped, &responseErr) || !strings.Contains(responseErr.Error(), "Gateway request failed: boom") {
		t.Fatalf("expected gateway response error, got %T: %v", wrapped, wrapped)
	}
}

type gatewayTimeoutTestError struct{}

func (gatewayTimeoutTestError) Error() string   { return "request timeout" }
func (gatewayTimeoutTestError) Timeout() bool   { return true }
func (gatewayTimeoutTestError) Temporary() bool { return false }
