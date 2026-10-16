package quiverai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS lets a transport failure (an abort) through unchanged; a made-up 500
// would make a cancelled call look retryable.
func TestDoGenerateCancelledContextIsNotAProviderError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ModelArrow11).DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "logo"})
	var perr *providererrors.ProviderError
	if !errors.Is(err, context.Canceled) || errors.As(err, &perr) {
		t.Fatalf("err = %#v", err)
	}
}
