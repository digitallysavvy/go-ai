package gateway

import (
	"context"
	"encoding/base64"
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
	if m.SpecificationVersion() != "v4" || m.Provider() != "gateway" || m.ModelID() != "openai/gpt-image-1" {
		t.Fatalf("metadata mismatch")
	}
	if m.MaxImagesPerCall() != 9007199254740991 {
		t.Fatalf("MaxImagesPerCall mismatch")
	}
	headers := m.getModelConfigHeaders()
	if headers["ai-image-model-specification-version"] != "4" || headers["ai-model-id"] != "openai/gpt-image-1" {
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
		if r.Header.Get("X-Request") != "req" {
			t.Fatalf("missing request header: %#v", r.Header)
		}
		w.Header().Set("X-Resp", "img")
		_, _ = w.Write([]byte(`{"images":["aW1n"],"warnings":[{"type":"unsupported","feature":"aspectRatio"}],"providerMetadata":{"gateway":{"images":[{"id":"1"}]}},"usage":{"inputTokens":1,"outputTokens":2,"totalTokens":3}}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	m := NewImageModel(p, "openai/gpt-image-1")
	n := 2
	seed := 7
	result, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:      "draw cat",
		N:           &n,
		Size:        "1024x1024",
		AspectRatio: "1:1",
		Seed:        &seed,
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"quality": "high"},
		},
		Files: []provider.ImageFile{
			{Type: "file", Data: []byte("raw"), MediaType: "image/png"},
			{Type: "file", Data: []byte{}},
		},
		Mask: &provider.ImageFile{Type: "file", Data: []byte("bWFzaw=="), MediaType: "image/png"},
		Headers: map[string]string{
			"X-Request": "req",
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if seenPath != "/v4/ai/image-model" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenBody["prompt"] != "draw cat" || seenBody["n"] != float64(2) || seenBody["size"] != "1024x1024" || seenBody["aspectRatio"] != "1:1" || seenBody["seed"] != float64(7) {
		t.Fatalf("body mismatch: %#v", seenBody)
	}
	if _, ok := seenBody["quality"]; ok {
		t.Fatalf("quality should stay under providerOptions, body=%#v", seenBody)
	}
	if seenBody["providerOptions"].(map[string]interface{})["openai"].(map[string]interface{})["quality"] != "high" {
		t.Fatalf("providerOptions mismatch: %#v", seenBody)
	}
	files := seenBody["files"].([]interface{})
	if files[0].(map[string]interface{})["data"] != "cmF3" || files[0].(map[string]interface{})["mediaType"] != "image/png" {
		t.Fatalf("files mismatch: %#v", seenBody)
	}
	if files[1].(map[string]interface{})["data"] != "" {
		t.Fatalf("explicit empty file data should be serialized as empty base64 string: %#v", seenBody)
	}
	if _, ok := files[1].(map[string]interface{})["mediaType"]; ok {
		t.Fatalf("mediaType should not be defaulted for empty file data: %#v", seenBody)
	}
	if seenBody["mask"].(map[string]interface{})["data"] != base64.StdEncoding.EncodeToString([]byte("bWFzaw==")) {
		t.Fatalf("mask bytes should be base64-encoded like TS Uint8Array data: %#v", seenBody)
	}
	if result.Base64Image != "aW1n" || string(result.Image) != "aW1n" || len(result.Warnings) != 1 || result.Usage.InputTokens != 1 || result.Usage.OutputTokens != 2 || result.Usage.TotalTokens != 3 {
		t.Fatalf("result mismatch: %#v", result)
	}
	if result.Response == nil || result.Response.ModelID != "openai/gpt-image-1" || result.Response.Headers["X-Resp"] != "img" {
		t.Fatalf("response metadata mismatch: %#v", result.Response)
	}
	if result.ProviderMetadata["gateway"].(map[string]interface{})["images"] == nil {
		t.Fatalf("provider metadata mismatch: %#v", result.ProviderMetadata)
	}
}

func TestGatewayImageModelProviderOptionsAndZeroSeedParity(t *testing.T) {
	var seenBody map[string]interface{}
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"images":["aW1n"]}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	seed := 0
	result, err := NewImageModel(p, "openai/gpt-image-1").DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Prompt:          "",
		Seed:            &seed,
		Files:           []provider.ImageFile{},
		ProviderOptions: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("DoGenerate error = %v", err)
	}
	if _, ok := seenBody["seed"]; ok {
		t.Fatalf("seed=0 should be omitted to match TS truthy spread behavior: %#v", seenBody)
	}
	if prompt, ok := seenBody["prompt"].(string); !ok || prompt != "" {
		t.Fatalf("empty prompt string should be preserved in request body: %#v", seenBody)
	}
	if providerOptions, ok := seenBody["providerOptions"].(map[string]interface{}); !ok || len(providerOptions) != 0 {
		t.Fatalf("empty providerOptions should be preserved when provided: %#v", seenBody)
	}
	if files, ok := seenBody["files"].([]interface{}); !ok || len(files) != 0 {
		t.Fatalf("explicit empty files slice should serialize as files: [] like TS: %#v", seenBody)
	}
	if result.Warnings == nil || len(result.Warnings) != 0 {
		t.Fatalf("warnings should be an explicit empty slice to match TS, got %#v", result.Warnings)
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
