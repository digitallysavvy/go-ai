package perplexity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// A 2xx embeddings response with `data: []` indexed [0] unchecked in DoEmbed
// and panicked. TS embed() rejects it with InvalidResponseDataError
// "No embedding generated.".
func TestR81provaPerplexityDoEmbedEmptyDataErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[],"model":"m","usage":{"prompt_tokens":1}}`))
	}))
	defer srv.Close()
	m := NewEmbeddingModel(New(Config{BaseURL: srv.URL, APIKey: "k"}), "pplx-embed-v1-4b")
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DoEmbed panicked: %v", r)
		}
	}()
	_, err := m.DoEmbed(context.Background(), "hi", nil)
	var invalid *providererrors.InvalidResponseDataError
	if !errors.As(err, &invalid) || invalid.Message != "No embedding generated." {
		t.Fatalf("err = %T %v, want InvalidResponseDataError \"No embedding generated.\"", err, err)
	}
}
