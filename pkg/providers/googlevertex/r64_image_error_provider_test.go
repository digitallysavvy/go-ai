package googlevertex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS google-vertex-image-model.ts builds its GoogleLanguageModel with the
// image model's provider (google.vertex.image), so a failed Gemini image call
// names the image model's provider, not the chat model's.
func TestR64VertexGeminiImageErrorNamesImageProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"bad request","status":"INVALID_ARGUMENT"}}`))
	}))
	defer server.Close()
	prov, err := New(Config{Project: "p", Location: "us-central1", AccessToken: "t", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewImageModel(prov, "gemini-2.5-flash-image").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %T %v, want a *ProviderError", err, err)
	}
	if pe.Provider != "google.vertex.image" {
		t.Fatalf("ProviderError.Provider = %q, want google.vertex.image", pe.Provider)
	}
}
