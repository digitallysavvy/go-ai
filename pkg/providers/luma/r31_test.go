package luma

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS postJsonToApi lets a transport failure (an abort) through unchanged.
func TestDoGenerateCancelledContextIsNotWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "photon-1").DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "x"})
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "failed to submit") {
		t.Fatalf("err = %v", err)
	}
}
